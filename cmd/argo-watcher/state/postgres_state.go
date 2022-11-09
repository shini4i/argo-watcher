package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/avast/retry-go/v4"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/lib/pq"
	"github.com/romana/rlog"
	"os"
	"time"

	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	h "github.com/shini4i/argo-watcher/internal/helpers"
	m "github.com/shini4i/argo-watcher/internal/models"
)

var (
	dbHost     = os.Getenv("DB_HOST")
	dbPort     = h.GetEnv("DB_PORT", "5432")
	dbName     = os.Getenv("DB_NAME")
	dbUser     = os.Getenv("DB_USER")
	dbPassword = os.Getenv("DB_PASSWORD")
)

type PostgresState struct {
	db *sql.DB
}

func (state *PostgresState) Connect() {
	c := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", dbHost, dbPort, dbUser, dbPassword, dbName)

	db, err := sql.Open("postgres", c)
	if err != nil {
		panic(err)
	}

	migrationsPath := fmt.Sprintf("file://%s", h.GetEnv("DB_MIGRATIONS_PATH", "db/migrations"))

	driver, _ := postgres.WithInstance(db, &postgres.Config{})
	migrations, _ := migrate.NewWithDatabaseInstance(
		migrationsPath,
		"postgres", driver)

	rlog.Debug("Running database migrations...")

	switch err = migrations.Up(); err {
	case migrate.ErrNoChange, nil:
		rlog.Debug("Database schema is up to date.")
	default:
		panic(err)
	}

	state.db = db
}

func (state *PostgresState) Add(task m.Task) {
	images, err := json.Marshal(task.Images)
	if err != nil {
		return
	}
	_, err = state.db.Exec("INSERT INTO tasks(id, created, images, status, app, author, project) VALUES ($1, $2, $3, $4, $5, $6, $7)",
		task.Id,
		time.Now().UTC(),
		images,
		"in progress",
		task.App,
		task.Author,
		task.Project,
	)

	if err != nil {
		panic(err)
	}
}

func (state *PostgresState) GetTasks(startTime float64, endTime float64, app string) []m.Task {
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
		rlog.Error(err)
		return nil
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			rlog.Error(err)
		}
	}(rows)

	var tasks []m.Task

	// This is required to handle potential null values in updated column
	var updated sql.NullFloat64
	// A temporary variable to store images column content
	var images []uint8

	for rows.Next() {
		var task m.Task

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
		return []m.Task{}
	} else {
		return tasks
	}
}

func (state *PostgresState) GetTaskStatus(id string) string {
	var status string

	err := state.db.QueryRow("SELECT status FROM tasks WHERE id=$1", id).Scan(&status)
	if err != nil {
		rlog.Errorf("Failed to get task status for task %s: %s", id, err)
		return "task not found"
	}

	return status
}

func (state *PostgresState) SetTaskStatus(id string, status string, reason string) {
	_, err := state.db.Exec("UPDATE tasks SET status=$1, status_reason=$2, updated=$3 WHERE id=$4", status, reason, time.Now().UTC(), id)
	if err != nil {
		rlog.Error(err)
	}
}

func (state *PostgresState) GetAppList() []string {
	var apps []string

	rows, err := state.db.Query("SELECT DISTINCT app FROM tasks")
	if err != nil {
		rlog.Error(err)
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {
			rlog.Error(err)
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

func (state *PostgresState) Check() bool {
	_, err := state.db.Exec("SELECT 1")
	if err != nil {
		rlog.Error(err)
		return false
	}
	return true
}

func (state *PostgresState) ProcessObsoleteTasks() {
	rlog.Debug("Starting watching for obsolete tasks...")
	err := retry.Do(
		func() error {
			_, err := state.db.Exec("DELETE FROM tasks WHERE status = 'app not found'")
			if err != nil {
				rlog.Error(err)
			}

			_, err = state.db.Exec("UPDATE tasks SET status='aborted' WHERE status = 'in progress' AND created < now() - interval '1 hour'")
			if err != nil {
				rlog.Error(err)
			}

			return errors.New("returning error to retry")
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(60*time.Minute),
		retry.Attempts(0),
	)

	if err != nil {
		rlog.Errorf("Couldn't process obsolete tasks. Got the following error: %s", err)
	}
}
