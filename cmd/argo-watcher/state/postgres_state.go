package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/golang-migrate/migrate/v4"
	_ "github.com/lib/pq"
	"github.com/rs/zerolog/log"

	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

type PostgresState struct {
	db *sql.DB
}

func (state *PostgresState) Connect(serverConfig *config.ServerConfig) {
	c := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable", serverConfig.DbHost, serverConfig.DbPort, serverConfig.DbUser, serverConfig.DbPassword, serverConfig.DbName)

	db, err := sql.Open("postgres", c)
	if err != nil {
		panic(err)
	}

	migrationsPath := fmt.Sprintf("file://%s", serverConfig.DbMigrationsPath)

	driver, _ := postgres.WithInstance(db, &postgres.Config{})
	migrations, _ := migrate.NewWithDatabaseInstance(
		migrationsPath,
		"postgres", driver)

	log.Debug().Msg("Running database migrations...")

	switch err = migrations.Up(); err {
	case migrate.ErrNoChange, nil:
		log.Debug().Msg("Database schema is up to date.")
	default:
		panic(err)
	}

	state.db = db
}

func (state *PostgresState) Add(task models.Task) error {
	images, err := json.Marshal(task.Images)
	if err != nil {
		return fmt.Errorf("could not marshal images into json")
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
		log.Error().Str("id", task.Id).Msgf("Failed to create task database record with error: %s", err)
		return fmt.Errorf("failed to create task in database")
	}

	return nil
}

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

func (state *PostgresState) SetTaskStatus(id string, status string, reason string) {
	_, err := state.db.Exec("UPDATE tasks SET status=$1, status_reason=$2, updated=$3 WHERE id=$4", status, reason, time.Now().UTC(), id)
	if err != nil {
		log.Error().Msg(err.Error())
	}
}

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

func (state *PostgresState) Check() bool {
	_, err := state.db.Exec("SELECT 1")
	if err != nil {
		log.Error().Msg(err.Error())
		return false
	}
	return true
}

func (state *PostgresState) ProcessObsoleteTasks() {
	log.Debug().Msg("Starting watching for obsolete tasks...")
	err := retry.Do(
		func() error {
			_, err := state.db.Exec("DELETE FROM tasks WHERE status = 'app not found'")
			if err != nil {
				log.Error().Msg(err.Error())
			}

			_, err = state.db.Exec("UPDATE tasks SET status='aborted' WHERE status = 'in progress' AND created < now() - interval '1 hour'")
			if err != nil {
				log.Error().Msg(err.Error())
			}

			return desiredRetryError
		},
		retry.DelayType(retry.FixedDelay),
		retry.Delay(60*time.Minute),
		retry.Attempts(0),
	)

	if err != nil {
		log.Error().Msgf("Couldn't process obsolete tasks. Got the following error: %s", err)
	}
}
