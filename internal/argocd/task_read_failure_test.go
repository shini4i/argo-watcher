package argocd

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/shini4i/argo-watcher/internal/mocks"
	"github.com/shini4i/argo-watcher/internal/models"
)

// detectRollback reads the app's deployed history to decide whether this
// deployment returns to an earlier image set. A backend that answered "no rows"
// for what was really an outage made every deployment look like a first one, and
// the wrong answer was then persisted on the task as IsRollback=false.
func TestArgoAddTask_ReadFailureDoesNotRecordAWrongRollbackFlag(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stateMock := newTaskRepositoryMock(ctrl)
	metricsMock := mocks.NewMockMetricsInterface(ctrl)

	readErr := errors.New("database unreachable")
	stateMock.EXPECT().GetTasks(gomock.Any()).Return(nil, int64(0), readErr)
	// Nothing else is declared: gomock fails the test if the submission is stored
	// or counted on history the backend never actually returned.

	argo := &Argo{}
	argo.Init(stateMock, newArgoApiMock(ctrl), metricsMock)

	newTask, err := argo.AddTask(models.Task{
		App:    "test-app",
		Images: []models.Image{{Image: "app", Tag: "v1"}},
	})

	require.Error(t, err, "a history read that failed must not be read as an empty history")
	assert.ErrorIs(t, err, readErr, "the cause must reach the caller, not be flattened")
	assert.Nil(t, newTask)
}

// The listing endpoint is the one read that must survive a backend failure
// without pretending: an empty list reads as "no deployments ran", which is the
// opposite of what an operator needs during an outage.
func TestArgoGetTasks_ReadFailureIsReportedNotSwallowed(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// The body is served unauthenticated when OIDC is off, so the driver's text —
	// DSN, schema, SQLSTATE — must stay in the server log.
	readErr := errors.New(`relation "tasks" does not exist (SQLSTATE 42P01) host=db.internal`)

	stateMock := newTaskRepositoryMock(ctrl)
	stateMock.EXPECT().GetTasks(gomock.Any()).Return(nil, int64(0), readErr)

	argo := &Argo{}
	argo.Init(stateMock, newArgoApiMock(ctrl), mocks.NewMockMetricsInterface(ctrl))

	response := argo.GetTasks(models.TaskFilter{EndTime: 1})

	assert.Equal(t, tasksFailedMessage, response.Error, "the client sees the fixed message, not the cause")
	assert.NotContains(t, response.Error, "SQLSTATE")
	assert.NotContains(t, response.Error, "db.internal")
	assert.Empty(t, response.Tasks)
	assert.Zero(t, response.Total, "a total alongside an error would read as a real count")
}

func TestArgoGetTasks_SuccessCarriesNoError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	stored := []models.Task{{Id: "a", App: "test-app"}}
	stateMock := newTaskRepositoryMock(ctrl)
	stateMock.EXPECT().GetTasks(gomock.Any()).Return(stored, int64(1), nil)

	argo := &Argo{}
	argo.Init(stateMock, newArgoApiMock(ctrl), mocks.NewMockMetricsInterface(ctrl))

	response := argo.GetTasks(models.TaskFilter{EndTime: 1})

	assert.Empty(t, response.Error)
	assert.Equal(t, stored, response.Tasks)
	assert.Equal(t, int64(1), response.Total)
}
