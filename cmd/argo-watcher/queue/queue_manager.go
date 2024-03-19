package queue

import (
	"sync"
	"time"

	"github.com/shini4i/argo-watcher/cmd/argo-watcher/argocd"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"
)

const (
	QUEUE_TYPE_IN_MEMORY = iota
	QUEUE_TYPE_RABBIT_MQ // TODO: implement Rabbit MQ polling
)

const (
	MAX_CONCURRENT_DEPLOYMENTS_PER_INSTANCE = 2
)

type QueueManager struct {
	// public fields
	Updater   *argocd.ArgoStatusUpdater
	QueueType int
	// common private fields
	inflightMutex   sync.Mutex
	inflightCounter int
	// In-Memory private fields
	tickerDone chan bool
	queueMutex sync.Mutex
	queueSlice []models.Task

	// Rabbit MQ private fields
	// ...
}

// Connect establishes a connection to the QueueManager
func (queueManager *QueueManager) Connect(serverConfig *config.ServerConfig) error {
	queueManager.QueueType = QUEUE_TYPE_IN_MEMORY
	if serverConfig.RabbitMq.Host != "" {
		queueManager.QueueType = QUEUE_TYPE_RABBIT_MQ
	}

	queueManager.inflightMutex = sync.Mutex{}
	queueManager.inflightCounter = 0

	if queueManager.QueueType == QUEUE_TYPE_IN_MEMORY {
		queueManager.queueMutex = sync.Mutex{}
	}

	// println("Queue type: " + strconv.Itoa(queueManager.QueueType))
	// println(serverConfig.RabbitMq.Host)
	// println(serverConfig.RabbitMq.Port)
	// println(serverConfig.RabbitMq.User)
	// println(serverConfig.RabbitMq.Password)
	return nil
}

func (queueManager *QueueManager) GetInflightCount() int {
	queueManager.inflightMutex.Lock()
	defer queueManager.inflightMutex.Unlock()
	return queueManager.inflightCounter
}

func (queueManager *QueueManager) InflightCountIncrease() {
	queueManager.inflightMutex.Lock()
	defer queueManager.inflightMutex.Unlock()
	queueManager.inflightCounter++
}

func (queueManager *QueueManager) InflightCountDecrease() {
	queueManager.inflightMutex.Lock()
	defer queueManager.inflightMutex.Unlock()
	queueManager.inflightCounter--
}

func (queueManager *QueueManager) QueueTask(task models.Task) {
	queueManager.queueMutex.Lock()
	defer queueManager.queueMutex.Unlock()
	queueManager.queueSlice = append(queueManager.queueSlice, task)
}

func (queueManager *QueueManager) PollTask() (task *models.Task, exists bool) {
	queueManager.queueMutex.Lock()
	defer queueManager.queueMutex.Unlock()
	if len(queueManager.queueSlice) == 0 {
		return nil, false
	}
	task = &queueManager.queueSlice[0]
	queueManager.queueSlice = queueManager.queueSlice[1:]
	return task, true
}

func (queueManager *QueueManager) StartListen() {
	queueManager.tickerDone = make(chan bool)
	ticker := time.NewTicker(1 * time.Second)

	go func() {
		for {
			select {
			case <-queueManager.tickerDone:
				return
			case <-ticker.C:
				for {
					// if 10 tasks already waiting for deployment - don't read any more tasks for now
					if queueManager.GetInflightCount() >= MAX_CONCURRENT_DEPLOYMENTS_PER_INSTANCE {
						break
					}
					// read task for the queue
					task, exists := queueManager.PollTask()
					if !exists {
						break
					}
					// increase count of running rollout routines
					queueManager.InflightCountIncrease()
					// create a go routine for the rollout
					go func() {
						queueManager.Updater.WaitForRollout(*task)
						// decrease inflight count when done
						queueManager.InflightCountDecrease()
					}()
				}
			}
		}
	}()
}

func (queueManager *QueueManager) StopListen() {
	queueManager.tickerDone <- true
}
