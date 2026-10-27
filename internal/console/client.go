package console

import (
	"log"

	"github.com/gorilla/websocket"
)

// Client is one connected console operator.
type Client struct {
	conn *websocket.Conn
	send chan []byte
	done chan struct{}
}

// NewClient wraps a WebSocket connection for the console.
func NewClient(conn *websocket.Conn) *Client {
	return &Client{
		conn: conn,
		send: make(chan []byte, 64),
		done: make(chan struct{}),
	}
}

// Send queues a message for the client. If the buffer is full, the message is
// dropped — a slow console must not stall a live call.
func (c *Client) Send(data []byte) {
	select {
	case c.send <- data:
	default:
		log.Printf("console: dropping message for slow client")
	}
}

// WritePump sends queued messages to the WebSocket. Runs until the client
// disconnects or the done channel is closed.
func (c *Client) WritePump() {
	defer c.conn.Close()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}

// ReadPump reads from the WebSocket until the client disconnects. The console
// is read-only in Phase 3, so inbound messages are discarded.
func (c *Client) ReadPump() {
	defer c.Close()
	c.conn.SetReadLimit(512)
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// Close signals the write pump to stop.
func (c *Client) Close() {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
}
