package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/argocd"
	"github.com/shini4i/argo-watcher/cmd/argo-watcher/config"
	"github.com/shini4i/argo-watcher/internal/models"

	amqp "github.com/rabbitmq/amqp091-go"
)

const (
	QUEUE_TYPE_IN_MEMORY = iota
	QUEUE_TYPE_RABBIT_MQ // TODO: implement Rabbit MQ polling
)

const (
	MAX_CONCURRENT_DEPLOYMENTS_PER_INSTANCE = 10
	QUEUE_NAME                              = "ArgoWatcherTaskQueue"
	QUEUE_CONSUMER                          = "ArgoWatcherConsumer"
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
	ampqConnection   *amqp.Connection
	ampqWriteChannel *amqp.Channel
	ampqReadChannel  *amqp.Channel
	ampqReadMessages <-chan amqp.Delivery
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
		log.Info().Msg("Initializing in-memory queue")
		queueManager.queueMutex = sync.Mutex{}
	}

	if queueManager.QueueType == QUEUE_TYPE_RABBIT_MQ {
		log.Info().Msg("Initializing RabbitMQ queue")

		// TODO: figure out how to handle connection breaks / reconnect - https://github.com/isayme/go-amqp-reconnect/blob/master/rabbitmq/rabbitmq.go
		var err error
		queueManager.ampqConnection, err = amqp.Dial(
			fmt.Sprintf("amqp://%s:%s@%s:%d/",
				serverConfig.RabbitMq.User,
				serverConfig.RabbitMq.Password,
				serverConfig.RabbitMq.Host,
				serverConfig.RabbitMq.Port,
			))
		if err != nil {
			return err
		}

		queueManager.ampqWriteChannel, err = queueManager.ampqConnection.Channel()
		if err != nil {
			return err
		}

		_, err = queueManager.ampqWriteChannel.QueueDeclare(
			QUEUE_NAME, // name
			false,      // durable
			false,      // delete when unused
			false,      // exclusive
			false,      // no-wait
			nil,        // arguments
		)
		if err != nil {
			return err
		}

		queueManager.ampqReadChannel, err = queueManager.ampqConnection.Channel()
		if err != nil {
			return err
		}

		queueManager.ampqReadMessages, err = queueManager.ampqReadChannel.Consume(
			QUEUE_NAME,     // queue
			QUEUE_CONSUMER, // consumer
			true,           // auto-ack - this removes the message from the queue as soon as we read it
			false,          // exclusive
			false,          // no-local
			false,          // no-wait
			nil,            // args
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (queueManager *QueueManager) GetInflightCount() int {
	defer queueManager.inflightMutex.Unlock()
	queueManager.inflightMutex.Lock()
	return queueManager.inflightCounter
}

func (queueManager *QueueManager) InflightCountIncrease() {
	defer queueManager.inflightMutex.Unlock()
	queueManager.inflightMutex.Lock()
	queueManager.inflightCounter++
}

func (queueManager *QueueManager) InflightCountDecrease() {
	defer queueManager.inflightMutex.Unlock()
	queueManager.inflightMutex.Lock()
	queueManager.inflightCounter--
}

func (queueManager *QueueManager) QueueTask(task models.Task) error {
	if queueManager.QueueType == QUEUE_TYPE_IN_MEMORY {
		defer queueManager.queueMutex.Unlock()
		queueManager.queueMutex.Lock()
		queueManager.queueSlice = append(queueManager.queueSlice, task)
	}
	if queueManager.QueueType == QUEUE_TYPE_RABBIT_MQ {
		// create context for publishing messages
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		encodedTask, err := json.Marshal(task)
		if err != nil {
			return err
		}

		err = queueManager.ampqWriteChannel.PublishWithContext(ctx,
			"",         // exchange
			QUEUE_NAME, // routing key
			false,      // mandatory
			false,      // immediate
			amqp.Publishing{
				ContentType: "text/plain",
				Body:        encodedTask,
			},
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func (queueManager *QueueManager) PollTask() (task *models.Task, exists bool) {
	if queueManager.QueueType == QUEUE_TYPE_IN_MEMORY {
		defer queueManager.queueMutex.Unlock()
		queueManager.queueMutex.Lock()
		if len(queueManager.queueSlice) == 0 {
			return nil, false
		}
		task = &queueManager.queueSlice[0]
		queueManager.queueSlice = queueManager.queueSlice[1:]
		return task, true
	}

	if queueManager.QueueType == QUEUE_TYPE_RABBIT_MQ {
		taskJson := <-queueManager.ampqReadMessages
		var task models.Task
		err := json.Unmarshal(taskJson.Body, &task)
		if err != nil {
			log.Error().Msgf("Error reading task from RabbitMQ. Unable to marshal JSON due to %s", err)
			return nil, false
		}
		return &task, true
	}

	return nil, false
}

func (queueManager *QueueManager) StartListen() error {
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
						// decrease inflight count when done
						defer queueManager.InflightCountDecrease()
						// rollout the task
						queueManager.Updater.WaitForRollout(*task)
					}()
				}
			}
		}
	}()

	return nil
}

func (queueManager *QueueManager) StopListen() {
	// stop the ticket
	queueManager.tickerDone <- true

	// disconnect from RabbitMQ
	if queueManager.QueueType == QUEUE_TYPE_RABBIT_MQ {
		queueManager.ampqConnection.Close()
		queueManager.ampqWriteChannel.Close()
		queueManager.ampqReadChannel.Close()
	}
}
