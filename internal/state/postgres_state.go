package state

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/state/state_models"
)

// whereStatusEquals is the GORM condition matching a task by its status column.
const whereStatusEquals = "status = ?"

type PostgresState struct {
	orm *gorm.DB
}

var _ TaskRepository = (*PostgresState)(nil)

// Connect establishes a connection to the PostgreSQL database using the provided server configuration.
func (state *PostgresState) Connect(serverConfig *config.ServerConfig) error {
	// create ORM driver
	if orm, err := gorm.Open(postgres.Open(serverConfig.Db.DSN)); err != nil {
		return err
	} else {
		state.orm = orm
	}
	return nil
}

// AddTask inserts a new task into the PostgreSQL database with the provided details.
// It takes a models.Task parameter and returns an error if the insertion fails.
// The method executes an INSERT query to add a new record with the task details, including the current UTC time.
func (state *PostgresState) AddTask(task models.Task) (*models.Task, error) {
	ormTask := state_models.TaskModel{
		Images:           datatypes.NewJSONSlice(task.Images),
		Status:           models.StatusInProgressMessage,
		ApplicationName:  sql.NullString{String: task.App, Valid: true},
		Author:           sql.NullString{String: task.Author, Valid: true},
		Project:          sql.NullString{String: task.Project, Valid: true},
		IsRollback:       task.IsRollback,
		RollbackTargetId: task.RollbackTargetId,
	}

	if err := state.orm.Create(&ormTask).Error; err != nil {
		slog.Error(fmt.Sprintf("Failed to create task database record with error: %s", err.Error()))
		return nil, fmt.Errorf("failed to create task in database")
	}

	// pass new values to the task object
	task.Id = ormTask.Id.String()
	task.Created = float64(ormTask.Created.UnixMilli())
	task.Status = models.StatusInProgressMessage

	return &task, nil
}

// GetTasks retrieves a list of tasks from the PostgreSQL database based on the provided time range, app, and status filters.
// Empty filter values (app == "" or status == "") are treated as wildcards.
func (state *PostgresState) GetTasks(startTime float64, endTime float64, app string, status string, limit int, offset int) ([]models.Task, int64) {
	startTimeUTC := time.Unix(int64(startTime), 0).UTC()
	endTimeUTC := time.Unix(int64(endTime), 0).UTC()

	query := state.orm.Model(&state_models.TaskModel{}).Where("created > ?", startTimeUTC).Where("created <= ?", endTimeUTC)
	if app != "" {
		query = query.Where(`"tasks"."app" = ?`, app)
	}
	if status != "" {
		query = query.Where(`"tasks"."status" = ?`, status)
	}

	countQuery := query.Session(&gorm.Session{})
	var total int64
	if err := countQuery.Count(&total).Error; err != nil {
		slog.Error(err.Error())
		return []models.Task{}, 0
	}

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	query = query.Order("created DESC")

	var ormTasks []state_models.TaskModel
	if err := query.Find(&ormTasks).Error; err != nil {
		slog.Error(err.Error())
		return []models.Task{}, 0
	}

	tasks := make([]models.Task, len(ormTasks))
	for i, ormTask := range ormTasks {
		tasks[i] = *ormTask.ConvertToExternalTask()
	}

	return tasks, total
}

// GetTask retrieves a task from the PostgreSQL database based on the provided task ID.
// It returns ErrTaskNotFound when the id is malformed or no matching row exists,
// and a wrapped error for any other retrieval failure so callers can distinguish
// 404 from 500.
// The method executes a SELECT query with the given task ID and scans the result into the task struct.
// It handles converting the created and updated timestamps to float64 values and unmarshalling the images from the database.
func (state *PostgresState) GetTask(id string) (*models.Task, error) {
	// The id column is a uuid, so a malformed id can never match a row. Treat it
	// as not-found instead of letting Postgres raise a syntax error, which would
	// otherwise surface as a client-triggerable HTTP 500 and ERROR-log noise.
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrTaskNotFound
	}

	var ormTask state_models.TaskModel
	if err := state.orm.Take(&ormTask, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, fmt.Errorf("error retrieving task with ID %s: %w", id, err)
	}
	return ormTask.ConvertToExternalTask(), nil
}

// SetTaskStatus updates the status, status_reason, and updated fields of a task in the PostgreSQL database.
// It takes the task ID, new status, and status reason as input parameters.
// The updated field is set to the current UTC time.
// Returns an error if the task ID is not found.
func (state *PostgresState) SetTaskStatus(id string, status string, reason string) error {
	uuidv4, err := uuid.Parse(id)
	if err != nil {
		return err
	}
	var ormTask = state_models.TaskModel{Id: uuidv4}
	result := state.orm.Model(ormTask).Updates(state_models.TaskModel{Status: status, StatusReason: sql.NullString{String: reason, Valid: true}})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("task not found")
	}

	return nil
}

// CancelInProgressTasks marks in-progress tasks for the given app as cancelled
// and returns how many rows were affected. A task is only cancelled when it
// shares at least one image name with the supplied images (tags ignored), so
// independent per-image deployments of the same app do not cancel each other.
// Because the image match is evaluated in Go, the in-progress tasks are first
// fetched, filtered by image overlap, then updated by id. The UPDATE re-checks
// the in-progress status so a task that finished between the two queries is not
// clobbered.
func (state *PostgresState) CancelInProgressTasks(app string, images []models.Image, reason string) (int64, error) {
	var candidates []state_models.TaskModel
	if err := state.orm.Model(&state_models.TaskModel{}).
		Where(`"tasks"."app" = ?`, app).
		Where(whereStatusEquals, models.StatusInProgressMessage).
		Find(&candidates).Error; err != nil {
		return 0, err
	}

	var ids []uuid.UUID
	for _, candidate := range candidates {
		if imageNamesOverlap(candidate.Images, images) {
			ids = append(ids, candidate.Id)
		}
	}
	if len(ids) == 0 {
		return 0, nil
	}

	result := state.orm.Model(&state_models.TaskModel{}).
		Where("id IN ?", ids).
		Where(whereStatusEquals, models.StatusInProgressMessage).
		Updates(state_models.TaskModel{
			Status:       models.StatusCancelledMessage,
			StatusReason: sql.NullString{String: reason, Valid: true},
		})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// Check verifies the connection to the PostgreSQL database by executing a simple test query.
// It returns true if the database connection is successful and the test query is executed without errors.
// It returns false if there is an error in the database connection or the test query execution.
func (state *PostgresState) Check() bool {
	connection, err := state.orm.DB()
	if err != nil {
		slog.Error(fmt.Sprintf("Failed to retrieve DB connection: %s", err.Error()))
		return false
	}

	if err = connection.Ping(); err != nil {
		slog.Error(fmt.Sprintf("Failed to ping DB: %s", err.Error()))
		return false
	}

	return true
}

// ProcessObsoleteTasks monitors and handles obsolete tasks in the PostgreSQL state.
// It initiates a process to remove tasks with a status of 'app not found' and mark tasks older than 1 hour as 'aborted'.
// The function utilizes retry logic to handle potential errors and retry the process if necessary.
// The retry interval is set to 60 minutes, and the retry attempts are set to 0 (no limit).
func (state *PostgresState) ProcessObsoleteTasks(retryTimes uint) {
	slog.Debug("Starting watching for obsolete tasks...")
	err := retry.Do(
		func() error {
			if err := state.doProcessPostgresObsoleteTasks(); err != nil {
				slog.Error(fmt.Sprintf("Couldn't process obsolete tasks. Got the following error: %s", err))
				return err
			}
			return errDesiredRetry
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(ObsoleteTaskCheckInterval),
		retry.Attempts(retryTimes),
	)

	if err != nil {
		slog.Error(fmt.Sprintf("Couldn't process obsolete tasks. Got the following error: %s", err))
	}
}

// processPostgresObsoleteTasks processes and handles obsolete tasks in the PostgreSQL database.
// It removes tasks with a status of 'app not found' and marks tasks older than 1 hour as 'aborted'.
// The function expects a valid *sql.DB connection to the PostgreSQL database.
// It returns an error if any database operation encounters an error; otherwise, it returns nil.
func (state *PostgresState) doProcessPostgresObsoleteTasks() error {
	slog.Debug("Removing obsolete tasks...")

	slog.Debug("Removing app not found tasks older than 1 hour from the database...")
	if err := state.orm.Where(whereStatusEquals, models.StatusAppNotFoundMessage).Where("created < now() - interval '1 hour'").Delete(&state_models.TaskModel{}).Error; err != nil {
		return err
	}

	slog.Debug("Marking in progress tasks older than 1 hour as aborted...")
	if err := state.orm.Where(whereStatusEquals, models.StatusInProgressMessage).Where("created < now() - interval '1 hour'").Updates(&state_models.TaskModel{Status: models.StatusAborted}).Error; err != nil {
		return err
	}

	return nil
}

// GetDB returns the underlying GORM database instance.
// This allows sharing the database connection pool with other components.
func (state *PostgresState) GetDB() *gorm.DB {
	return state.orm
}
