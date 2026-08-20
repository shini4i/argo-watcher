package state

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/google/uuid"

	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/shini4i/argo-watcher/internal/config"
	"github.com/shini4i/argo-watcher/internal/models"
	"github.com/shini4i/argo-watcher/internal/state/state_models"
)

const whereStatusEquals = "status = ?"

// retentionDeleteBatchSize is how many expired tasks one DELETE removes. It
// keeps each statement short enough not to hold locks or grow a transaction for
// long, while still draining a large backlog in few enough round trips.
const retentionDeleteBatchSize = 1000

type PostgresState struct {
	orm *gorm.DB
	// ownerId identifies this process as the holder of task leases. It is unique
	// per process, so a restarted pod never inherits its predecessor's claims.
	ownerId string
	// retentionEnabled and retentionDays configure the removal of finished tasks
	// older than the window, performed by the obsolete-task sweep.
	retentionEnabled bool
	retentionDays    int
}

var _ TaskRepository = (*PostgresState)(nil)

// Connect establishes a connection to the PostgreSQL database using the provided server configuration.
// It emits an INFO log before dialing (the first line at the default log level, so a stalled
// connection is diagnosable) and relies on the DSN's connect_timeout to fail fast when Postgres is
// unreachable rather than blocking on the OS TCP timeout.
func (state *PostgresState) Connect(serverConfig *config.ServerConfig) error {
	slog.Info("Connecting to PostgreSQL database...")
	if orm, err := gorm.Open(postgres.Open(serverConfig.Db.DSN)); err != nil {
		return err
	} else {
		state.orm = orm
	}

	ownerId, err := newOwnerId()
	if err != nil {
		return err
	}
	state.ownerId = ownerId
	slog.Debug("Task lease owner id assigned", "owner_id", ownerId)

	state.retentionEnabled = serverConfig.TaskRetentionEnabled
	state.retentionDays = serverConfig.TaskRetentionDays
	if state.retentionEnabled {
		slog.Info("Task retention is enabled", "retention_days", state.retentionDays)
	}

	return nil
}

// AddTask returns the task with the DB-generated id and creation time.
func (state *PostgresState) AddTask(task models.Task) (*models.Task, error) {
	ormTask := state_models.TaskModel{
		Images:           datatypes.NewJSONSlice(task.Images),
		Status:           models.StatusInProgressMessage,
		ApplicationName:  sql.NullString{String: task.App, Valid: true},
		Author:           sql.NullString{String: task.Author, Valid: true},
		Project:          sql.NullString{String: task.Project, Valid: true},
		IsRollback:       task.IsRollback,
		RollbackTargetId: task.RollbackTargetId,
		Validated:        task.Validated,
		Timeout:          task.Timeout,
		Refresh:          nullBoolFromPointer(task.Refresh),
	}

	if err := state.orm.Create(&ormTask).Error; err != nil {
		slog.Error("Failed to create task database record", "error", err)
		return nil, fmt.Errorf("failed to create task in database")
	}

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
		slog.Error("Failed to count tasks", "error", err)
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
		slog.Error("Failed to query tasks", "error", err)
		return []models.Task{}, 0
	}

	tasks := make([]models.Task, len(ormTasks))
	for i, ormTask := range ormTasks {
		tasks[i] = *ormTask.ConvertToExternalTask()
	}

	return tasks, total
}

// GetTask retrieves a task by id. It returns ErrTaskNotFound when the id is
// malformed or no matching row exists, and a wrapped error for any other
// retrieval failure so callers can distinguish 404 from 500.
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

// SetTaskStatus errors if the id is malformed or no matching task exists.
func (state *PostgresState) SetTaskStatus(id, status, reason string) error {
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
// independent per-image deployments of the same app do not cancel each other,
// and only when it carries no more authority than the superseding deployment.
// Because both checks are evaluated in Go, the in-progress tasks are first
// fetched, filtered, then updated by id. The UPDATE re-checks the in-progress
// status so a task that finished between the two queries is not clobbered.
func (state *PostgresState) CancelInProgressTasks(app string, images []models.Image, reason string, newTaskValidated bool) (int64, error) {
	var candidates []state_models.TaskModel
	if err := state.orm.Model(&state_models.TaskModel{}).
		Where(`"tasks"."app" = ?`, app).
		Where(whereStatusEquals, models.StatusInProgressMessage).
		Find(&candidates).Error; err != nil {
		return 0, err
	}

	var ids []uuid.UUID
	for _, candidate := range candidates {
		if maySupersede(newTaskValidated, candidate.Validated) && imageNamesOverlap(candidate.Images, images) {
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

// Check reports whether the database connection is alive.
func (state *PostgresState) Check() bool {
	connection, err := state.orm.DB()
	if err != nil {
		slog.Error("Failed to retrieve DB connection", "error", err)
		return false
	}

	if err = connection.Ping(); err != nil {
		slog.Error("Failed to ping DB", "error", err)
		return false
	}

	return true
}

// ProcessObsoleteTasks runs doProcessPostgresObsoleteTasks every
// ObsoleteTaskCheckInterval. retryTimes bounds the number of runs; 0 means run
// forever (the production case). It is meant to run in its own goroutine.
func (state *PostgresState) ProcessObsoleteTasks(retryTimes uint) {
	slog.Debug("Starting watching for obsolete tasks...")
	err := retry.Do(
		func() error {
			if err := state.doProcessPostgresObsoleteTasks(); err != nil {
				slog.Error("Couldn't process obsolete tasks", "error", err)
				return err
			}
			return errDesiredRetry
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(ObsoleteTaskCheckInterval),
		retry.Attempts(retryTimes),
	)

	if err != nil {
		slog.Error("Couldn't process obsolete tasks", "error", err)
	}
}

func (state *PostgresState) doProcessPostgresObsoleteTasks() error {
	slog.Debug("Removing obsolete tasks...")

	slog.Debug("Removing app not found tasks older than 1 hour from the database...")
	if err := state.orm.Where(whereStatusEquals, models.StatusAppNotFoundMessage).Where("created < now() - interval '1 hour'").Delete(&state_models.TaskModel{}).Error; err != nil {
		return err
	}

	slog.Debug("Marking in progress tasks older than 1 hour as aborted...")
	if err := state.orm.Where(whereStatusEquals, models.StatusInProgressMessage).Where("created < now() - interval '1 hour'").Updates(&state_models.TaskModel{
		Status:       models.StatusAborted,
		StatusReason: sql.NullString{String: StaleTaskAbortReason, Valid: true},
	}).Error; err != nil {
		return err
	}

	// Runs last so a task this same pass aborted for staleness is already eligible,
	// which only matters for a retention window short enough to overlap it.
	if err := state.deleteExpiredTasks(); err != nil {
		return err
	}

	return nil
}

// deleteExpiredTasks removes finished tasks created longer ago than the
// retention window, and does nothing when retention is disabled.
//
// Rows are deleted in batches instead of one statement: the first sweep after
// enabling retention can face a table holding every deployment ever made, and a
// single DELETE over it would hold row locks and bloat one transaction for as
// long as it ran. SKIP LOCKED lets replicas sweeping concurrently step over each
// other's batches rather than queue behind them.
//
// In-progress tasks are never deleted, whatever their age: one may be claimed
// and actively monitored by a replica, which would then write a status for a row
// that no longer exists.
func (state *PostgresState) deleteExpiredTasks() error {
	if !state.retentionEnabled {
		return nil
	}

	slog.Debug("Removing tasks older than the retention window...", "retention_days", state.retentionDays)

	var deleted int64
	for {
		result := state.orm.Exec(`
			DELETE FROM tasks
			WHERE id IN (
				SELECT id FROM tasks
				WHERE created < now() - make_interval(days => ?)
					AND status <> ?
				ORDER BY created
				LIMIT ?
				FOR UPDATE SKIP LOCKED
			)`, state.retentionDays, models.StatusInProgressMessage, retentionDeleteBatchSize)
		if result.Error != nil {
			// Named because the sweep runs three steps and reports their failures
			// through one log line, where a bare driver error says nothing about which
			// step produced it.
			return fmt.Errorf("deleting tasks past the %d day retention window: %w", state.retentionDays, result.Error)
		}

		deleted += result.RowsAffected
		if result.RowsAffected < retentionDeleteBatchSize {
			break
		}
	}

	if deleted > 0 {
		slog.Info("Removed tasks older than the retention window", "count", deleted, "retention_days", state.retentionDays)
	}

	return nil
}

// GetDB exposes the connection pool so other components can share it.
func (state *PostgresState) GetDB() *gorm.DB {
	return state.orm
}

// nullBoolFromPointer maps an optional override onto its nullable column: an
// omitted field stays NULL, which is distinct from an explicit false.
func nullBoolFromPointer(value *bool) sql.NullBool {
	if value == nil {
		return sql.NullBool{}
	}

	return sql.NullBool{Bool: *value, Valid: true}
}
