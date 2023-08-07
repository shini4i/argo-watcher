package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/avast/retry-go/v4"
	_ "github.com/lib/pq"

	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/state/state_models"
	"github.com/shini4i/argo-watcher/internal/models"
)

type PostgresState struct {
	db  *sql.DB // for backwards compatibility. NOTE: note save when using multiple connections in ORM (connection POOL or reconnecting)
	orm *gorm.DB
}

// Connect establishes a connection to the PostgreSQL database using the provided server configuration.
func (state *PostgresState) Connect(serverConfig *config.ServerConfig) error {
	dsnTemplate := "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable TimeZone=UTC"
	dsn := fmt.Sprintf(dsnTemplate, serverConfig.DbHost, serverConfig.DbPort, serverConfig.DbUser, serverConfig.DbPassword, serverConfig.DbName)

	// create connection
	ormConfig := &gorm.Config{}
	// we can leave logger enabled only for text format
	if serverConfig.LogFormat != config.LOG_FORMAT_TEXT {
		// disable logging until we implement zerolog logger for ORM
		ormConfig.Logger = logger.Default.LogMode(logger.Silent)
	}
	orm, err := gorm.Open(postgres.Open(dsn), ormConfig)
	if err != nil {
		return err
	}

	// save orm object
	state.orm = orm

	// run migrations
	err = orm.AutoMigrate(&state_models.TaskModel{})
	if err != nil {
		return err
	}

	// save connection for backwards compatibility
	state.db, err = orm.DB()
	if err != nil {
		return err
	}

	return nil
}

// Add inserts a new task into the PostgreSQL database with the provided details.
// It takes a models.Task parameter and returns an error if the insertion fails.
// The method executes an INSERT query to add a new record with the task details, including the current UTC time.
func (state *PostgresState) Add(task models.Task) (*models.Task, error) {
	ormTask := state_models.TaskModel{
		Images:          datatypes.NewJSONSlice(task.Images),
		Status:          models.StatusInProgressMessage,
		ApplicationName: sql.NullString{String: task.App, Valid: true},
		Author:          sql.NullString{String: task.Author, Valid: true},
		Project:         sql.NullString{String: task.Project, Valid: true},
	}

	result := state.orm.Create(&ormTask)
	if result.Error != nil {
		log.Error().Msgf("Failed to create task database record with error: %s", result.Error)
		return nil, fmt.Errorf("failed to create task in database")
	}

	log.Info().Msg(ormTask.Id.String())

	// pass new values to the task object
	task.Id = ormTask.Id.String()
	task.Created = float64(ormTask.Created.UnixMilli())

	return &task, nil
}

// GetTasks retrieves a list of tasks from the PostgreSQL database based on the provided time range and optional app filter.
// It returns the tasks as a slice of models.Task.
// Tasks are filtered based on the time range (start time and end time) and, optionally, the app value.
// The method handles converting timestamps and unmarshalling images from the database.
func (state *PostgresState) GetTasks(startTime float64, endTime float64, app string) []models.Task {
	startTimeUTC := time.Unix(int64(startTime), 0).UTC()
	endTimeUTC := time.Unix(int64(endTime), 0).UTC()

	var rows *sql.Rows
	var err error

	if app == "" {
		rows, err = state.db.Query(
			"select id, extract(epoch from created) AS created, "+
				"extract(epoch from updated) AS updated, "+
				"images, status, status_reason, app, author, "+
				"project from tasks where created >= $1 AND created <= $2",
			startTimeUTC, endTimeUTC)
	} else {
		rows, err = state.db.Query(
			"select id, extract(epoch from created) AS created, "+
				"extract(epoch from updated) AS updated, "+
				"images, status, status_reason, app, author, "+
				"project from tasks where created >= $1 AND created <= $2 AND app = $3",
			startTimeUTC, endTimeUTC, app)
	}

	if err != nil {
		log.Error().Msg(err.Error())
		return nil
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Error().Msg(err.Error())
		}
	}(rows)

	var tasks []models.Task

	// This is required to handle potential null values in updated column
	var updated sql.NullFloat64
	// A temporary variable to store images column content
	var images []uint8

	for rows.Next() {
		var task models.Task

		if err := rows.Scan(&task.Id, &task.Created, &updated, &images, &task.Status, &task.StatusReason, &task.App, &task.Author, &task.Project); err != nil {
			panic(err)
		}

		if updated.Valid {
			task.Updated = updated.Float64
		}

		if err := json.Unmarshal(images, &task.Images); err != nil {
			panic(err)
		}

		tasks = append(tasks, task)
	}

	if tasks == nil {
		return []models.Task{}
	} else {
		return tasks
	}
}

// GetTask retrieves a task from the PostgreSQL database based on the provided task ID.
// It returns the task as a pointer to models.Task and an error if the task is not found or an error occurs during retrieval.
// The method executes a SELECT query with the given task ID and scans the result into the task struct.
// It handles converting the created and updated timestamps to float64 values and unmarshalling the images from the database.
func (state *PostgresState) GetTask(id string) (*models.Task, error) {
	var (
		task        models.Task
		imagesBytes []uint8
		images      []models.Image
		createdStr  string
		updatedNull sql.NullTime
		created     time.Time
		err         error
	)

	query := `
		SELECT id, status, status_reason, app, author, project, images, created, updated
		FROM tasks
	    WHERE id=$1
	`

	row := state.db.QueryRow(query, id)

	if err := row.Scan(&task.Id, &task.Status, &task.StatusReason, &task.App, &task.Author, &task.Project, &imagesBytes, &createdStr, &updatedNull); err != nil {
		return nil, err
	}

	if err := json.Unmarshal(imagesBytes, &images); err != nil {
		return nil, err
	}

	task.Images = images

	if created, err = time.Parse(time.RFC3339, createdStr); err != nil {
		return nil, err
	}
	task.Created = float64(created.Unix())

	if updatedNull.Valid {
		updatedFloat := updatedNull.Time.Unix()
		task.Updated = float64(updatedFloat)
	}

	return &task, nil
}

// SetTaskStatus updates the status, status_reason, and updated fields of a task in the PostgreSQL database.
// It takes the task ID, new status, and status reason as input parameters.
// The updated field is set to the current UTC time.
func (state *PostgresState) SetTaskStatus(id string, status string, reason string) {
	_, err := state.db.Exec("UPDATE tasks SET status=$1, status_reason=$2, updated=$3 WHERE id=$4", status, reason, time.Now().UTC(), id)
	if err != nil {
		log.Error().Msg(err.Error())
	}
}

// GetAppList retrieves a list of distinct application names from the tasks table in the PostgreSQL database.
// It executes a SELECT query to fetch the distinct app values and returns them as a slice of strings.
// If an error occurs during the database query or result processing, it logs the error and returns an empty slice.
func (state *PostgresState) GetAppList() []string {
	var apps []string

	rows, err := state.db.Query("SELECT DISTINCT app FROM tasks")
	if err != nil {
		log.Error().Msg(err.Error())
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			log.Error().Msg(err.Error())
		}
	}(rows)

	for rows.Next() {
		var app string
		if err := rows.Scan(&app); err != nil {
			panic(err)
		}
		apps = append(apps, app)
	}

	if apps == nil {
		return []string{}
	}

	return apps
}

// Check verifies the connection to the PostgreSQL database by executing a simple test query.
// It returns true if the database connection is successful and the test query is executed without errors.
// It returns false if there is an error in the database connection or the test query execution.
func (state *PostgresState) Check() bool {
	_, err := state.db.Exec("SELECT 1")
	if err != nil {
		log.Error().Msg(err.Error())
		return false
	}
	return true
}

// ProcessObsoleteTasks monitors and handles obsolete tasks in the PostgreSQL state.
// It initiates a process to remove tasks with a status of 'app not found' and mark tasks older than 1 hour as 'aborted'.
// The function utilizes retry logic to handle potential errors and retry the process if necessary.
// The retry interval is set to 60 minutes, and the retry attempts are set to 0 (no limit).
func (state *PostgresState) ProcessObsoleteTasks(retryTimes uint) {
	log.Debug().Msg("Starting watching for obsolete tasks...")
	err := retry.Do(
		func() error {
			if err := processPostgresObsoleteTasks(state.db); err != nil {
				log.Error().Msgf("Couldn't process obsolete tasks. Got the following error: %s", err)
				return err
			}
			return errDesiredRetry
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(60*time.Minute),
		retry.Attempts(retryTimes),
	)

	if err != nil {
		log.Error().Msgf("Couldn't process obsolete tasks. Got the following error: %s", err)
	}
}

// processPostgresObsoleteTasks processes and handles obsolete tasks in the PostgreSQL database.
// It removes tasks with a status of 'app not found' and marks tasks older than 1 hour as 'aborted'.
// The function expects a valid *sql.DB connection to the PostgreSQL database.
// It returns an error if any database operation encounters an error; otherwise, it returns nil.
func processPostgresObsoleteTasks(db *sql.DB) error {
	log.Debug().Msg("Removing obsolete tasks...")
	if _, err := db.Exec("DELETE FROM tasks WHERE status = 'app not found'"); err != nil {
		return err
	}

	log.Debug().Msg("Marking tasks older than 1 hour as aborted...")
	if _, err := db.Exec("UPDATE tasks SET status='aborted' WHERE status = 'in progress' AND created < now() - interval '1 hour'"); err != nil {
		return err
	}

	return nil
}
