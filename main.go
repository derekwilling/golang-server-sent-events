package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

// Broker contains all thinkgs for brokering.
type Broker struct {
	// Events are pushed to this channel by the main events-gathering routine.
	Notifier chan []byte
	// New client connections.
	newClients chan chan []byte
	// Closed client connections.
	closingClients chan chan []byte
	// Client connections registry.
	clients map[chan []byte]bool
}

// Listen on different channels and act accordingly.
func (broker *Broker) listen() {
	for {
		select {
		case s := <-broker.newClients:
			// A new client has connected.
			broker.clients[s] = true
			log.Printf("Client added. %d registered clients", len(broker.clients))

		case s := <-broker.closingClients:
			// A client has detached. Stop sending them messages.
			delete(broker.clients, s)
			log.Printf("Removed client. %d registered clients", len(broker.clients))

		case event := <-broker.Notifier:
			// We got a new event from outside!
			// Send event to all connected clients.
			for clientMsgChan, _ := range broker.clients {
				clientMsgChan <- event
			}
		}
	}
}

// Broker implements http.Handler to handle HTTP connections.
func (broker *Broker) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// Check if we can flush buffered data down the connection as it comes.
	flusher, ok := rw.(http.Flusher)
	if !ok {
		http.Error(rw, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	// Set basic headers to support keep-alive HTTP connections and the
	// "text/event-stream" content type.
	rw.Header().Set("Content-Type", "text/event-stream")
	rw.Header().Set("Cache-Control", "no-cache")
	rw.Header().Set("Connection", "keep-alive")
	rw.Header().Set("Access-Control-Allow-Origin", "*")

	// Each connection registers its own msg chan w/ the Broker's connections registry.
	msgChan := make(chan []byte)
	// Signal the broker that we have a new connection.
	broker.newClients <- msgChan
	// Ensure client is removed from the map of connected clients when handler exits.
	defer func() {
		broker.closingClients <- msgChan
	}()

	// Listen to connection close and un-register msgChan
	// notify := rw.(http.CloseNotifier).CloseNotify()
	go func() {
		<-req.Context().Done()
		broker.closingClients <- msgChan
	}()

	// block waiting for msgs broadcast on this connection's msgChan
	for {
		fmt.Fprintf(rw, "data: %s\n\n", <-msgChan)
		// Flush the data out the buffer immediately instead of buffering it for later.
		flusher.Flush()
	}
}

func NewServer() (broker *Broker) {
	broker = &Broker{
		Notifier:       make(chan []byte, 1),
		newClients:     make(chan chan []byte),
		closingClients: make(chan chan []byte),
		clients:        make(map[chan []byte]bool),
	}

	go broker.listen()

	return
}

func main() {
	broker := NewServer()

	// Push events out to all clients at regular intervals.
	go func() {
		for {
			time.Sleep(time.Second * 2)
			eventString := fmt.Sprintf("the time is %v", time.Now())
			log.Printf("Sending event to %d clients\n\n", len(broker.clients))
			broker.Notifier <- []byte(eventString)
		}
	}()

	log.Fatal("HTTP server error: ", http.ListenAndServe("localhost:8080", broker))
}
