package server

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"
)

var (
	connectionsMutex sync.Mutex
	connections      []*websocket.Conn
)

// handleWebSocketConnection accepts a WebSocket connection, adds it to a slice,
// and initiates a goroutine to ping the connection regularly. If WebSocket
// acceptance fails, an error is logged. The goroutine serves to monitor
// the connection's activity and removes it from the slice if it's inactive.
func (env *Env) handleWebSocketConnection(c *gin.Context) {
	options := &websocket.AcceptOptions{
		InsecureSkipVerify: env.config.DevEnvironment, // It will disable websocket host validation if set to true
	}

	conn, err := websocket.Accept(c.Writer, c.Request, options)
	if err != nil {
		log.Error().Msgf("couldn't accept websocket connection, got the following error: %s", err)
		return
	}

	connectionsMutex.Lock()
	connections = append(connections, conn)
	connectionsMutex.Unlock()

	go env.checkConnection(conn)
}

// checkConnection is a method for the Env struct that continuously checks the
// health of a WebSocket connection by sending periodic "heartbeat" messages.
func (env *Env) checkConnection(c *websocket.Conn) {
	ticker := time.NewTicker(time.Second * 30)
	defer ticker.Stop()

	for range ticker.C {
		// we are not using c.Ping here, because it's not working as expected
		// for some reason it's failing even if the connection is still alive
		// if you know how to fix it, please open an issue or PR
		if err := c.Write(context.Background(), websocket.MessageText, []byte("heartbeat")); err != nil {
			log.Debug().Err(err).Msg("websocket heartbeat failed, removing connection")
			_ = c.Close(websocket.StatusGoingAway, "heartbeat failed")
			removeWebSocketConnection(c)
			return
		}
	}
}

// notifyWebSocketClients is a function that sends a message to all active WebSocket clients.
// It iterates over the global connections slice, which contains all active WebSocket connections,
// and sends the provided message to each connection using the wsjson.Write function.
// If an error occurs while sending the message to a connection (for example, if the connection has been closed),
// it removes the connection from the connections slice to prevent further attempts to send messages to it.
func notifyWebSocketClients(message string) {
	var wg sync.WaitGroup

	connectionsMutex.Lock()
	snapshot := make([]*websocket.Conn, len(connections))
	copy(snapshot, connections)
	connectionsMutex.Unlock()

	for _, conn := range snapshot {
		wg.Add(1)

		go func(c *websocket.Conn, message string) {
			defer wg.Done()
			if err := c.Write(context.Background(), websocket.MessageText, []byte(message)); err != nil {
				log.Debug().Err(err).Msg("websocket broadcast failed, removing connection")
				removeWebSocketConnection(c)
			}
		}(conn, message)
	}

	wg.Wait()
}

// removeWebSocketConnection is a helper function that removes a WebSocket connection
// from the global connections slice. It is used to clean up connections that are no longer active.
// The function takes a WebSocket connection as an argument and removes it from the connections slice.
// It uses a mutex to prevent concurrent access to the connections slice, ensuring thread safety.
func removeWebSocketConnection(conn *websocket.Conn) {
	connectionsMutex.Lock()
	defer connectionsMutex.Unlock()

	for i := range connections {
		if connections[i] == conn {
			connections = append(connections[:i], connections[i+1:]...)
			break
		}
	}
}
