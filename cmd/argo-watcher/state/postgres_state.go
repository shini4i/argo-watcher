package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/lib/pq"
	"github.com/romana/rlog"
	"math"
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

func (p *PostgresState) Connect() {
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
	case migrate.ErrNoChange:
		rlog.Debug("Database schema is up to date.")
	case nil:
		rlog.Debug("Database schema is up to date.")
	default:
		panic(err)
	}

	p.db = db
}

func (p *PostgresState) Add(task m.Task) {
	images, err := json.Marshal(task.Images)
	if err != nil {
		return
	}
	_, err = p.db.Exec("INSERT INTO tasks(id, created, images, status, app, author, project) VALUES ($1, $2, $3, $4, $5, $6, $7)",
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

func (p *PostgresState) GetTasks(startTime float64, endTime float64, app string) []m.Task {
	integ, decim := math.Modf(startTime)
	start := time.Unix(int64(integ), int64(decim*(1e9)))

	integ, decim = math.Modf(endTime)
	end := time.Unix(int64(integ), int64(decim*(1e9)))

	var rows *sql.Rows
	var err error

	if app == "" {
		rows, err = p.db.Query(
			"select extract(epoch from created) AS created, "+
				"extract(epoch from updated) AS updated, "+
				"images, status, app, author, "+
				"project from tasks where created >= $1 AND created <= $2",
			start, end)
	} else {
		rows, err = p.db.Query(
			"select extract(epoch from created) AS created, "+
				"extract(epoch from updated) AS updated, "+
				"images, status, app, author, "+
				"project from tasks where created >= $1 AND created <= $2 AND app = $3",
			start, end, app)
	}

	if err != nil {
		rlog.Error(err)
		return nil
	}

	defer func(rows *sql.Rows) {
		err := rows.Close()
		if err != nil {

		}
	}(rows)

	var tasks []m.Task

	// This is required to handle potential null values in updated column
	var updated sql.NullFloat64
	// A temporary variable to store images column content
	var images []uint8

	for rows.Next() {
		var t m.Task
		// id is skipped as it is not used in the UI
		if err := rows.Scan(&t.Created, &updated, &images, &t.Status, &t.App, &t.Author, &t.Project); err != nil {
			rlog.Error(err)
			return nil
		}

		if updated.Valid {
			t.Updated = updated.Float64
		}

		err := json.Unmarshal(images, &t.Images)
		if err != nil {
			panic(err)
		}
		tasks = append(tasks, t)
	}

	if tasks == nil {
		return []m.Task{}
	} else {
		return tasks
	}
}

func (p *PostgresState) GetTaskStatus(id string) string {
	var status string

	err := p.db.QueryRow("SELECT status FROM tasks WHERE id=$1", id).Scan(&status)
	if err != nil {
		return "task not found"
	}

	return status
}

func (p *PostgresState) SetTaskStatus(id string, status string) {
	_, err := p.db.Exec("UPDATE tasks SET status=$1, updated=$2 where id=$3", status, time.Now().UTC(), id)
	if err != nil {
		rlog.Error(err)
	}
}

func (p *PostgresState) GetAppList() []string {
	var apps []string

	rows, err := p.db.Query("SELECT DISTINCT app FROM tasks")
	if err != nil {
		rlog.Error(err)
	}

	for rows.Next() {
		var app sql.NullString

		if err := rows.Scan(&app); err != nil {
			rlog.Error(err)
		}

		if app.Valid {
			apps = append(apps, app.String)
		}
	}

	if apps == nil {
		return []string{}
	} else {
		return apps
	}
}

func (p *PostgresState) Check() bool {
	err := p.db.Ping()
	if err != nil {
		rlog.Error(err)
		return false
	} else {
		return true
	}
}
