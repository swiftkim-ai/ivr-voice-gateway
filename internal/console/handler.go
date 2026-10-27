package console

import (
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(*http.Request) bool { return true },
}

// Handler returns an HTTP handler that upgrades to the console WebSocket.
func Handler(hub *Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		c := NewClient(conn)
		hub.Register(c)
		go c.WritePump()
		c.ReadPump() // blocks until disconnect
		hub.Unregister(c)
	}
}
