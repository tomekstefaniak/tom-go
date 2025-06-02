package sync

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"
)

type ReadMessage[T any] struct {
	Res chan T // Channel to send the read result
}

type WriteMessage[T any] struct {
	Data T // Data to write
}

type RWMonitor[T any] struct {
	reqsQueue         *list.List       // reqsQueue of processes waiting to read or write
	ReqsChan          chan interface{} // Channel to receive requests

	readFunc          func() T         // Function to read data
	readersCount      int              // Number of readers currently reading
	readersCountMutex sync.Mutex       // Mutex to protect access to readers field
	resTimeoutMs      int              // Timeout for read operations in milliseconds
	readersEnded      chan struct{}    // Channel to signal that all readers have finished

	writeFunc         func(T)          // Function to write data
	writing           int32            // Change writing flag to atomic int
	writerEnded       chan struct{}    // Channel to signal the end of a write operation

	StopRWMonitor     chan struct{}    // Channel to stop the RWMonitor
}

func CreateRWMonitor[T any](
	readFunc func() T,
	writeFunc func(T),
	resTimeoutMs int,
) *RWMonitor[T] {
	return &RWMonitor[T]{
		reqsQueue: list.New(),
		ReqsChan:  make(chan interface{}),

		readFunc:          readFunc,
		readersCount:      0,
		readersCountMutex: sync.Mutex{},
		readersEnded:      make(chan struct{}),
		resTimeoutMs:      resTimeoutMs,

		writeFunc:   writeFunc,
		writing:     0,
		writerEnded: make(chan struct{}),

		StopRWMonitor: make(chan struct{}),
	}
}

// Function that enwraps the read operation
func (m *RWMonitor[T]) read(res chan T) {
	data := m.readFunc() // Read the data
	select {
	case res <- data: // Send the read result
	case <-time.After(time.Duration(m.resTimeoutMs * int(time.Millisecond))): // If the read operation times out
		// Handle timeout if necessary, e.g., log or send an error
	}

	m.readersCountMutex.Lock()
	m.readersCount-- // Decrement the readers count
	if m.readersCount == 0 {
		m.readersEnded <- struct{}{} // Signal that all readers have finished
	}
	m.readersCountMutex.Unlock()
}

// Function that enwraps the write operation
func (m *RWMonitor[T]) write(data T) {
	m.writeFunc(data) // Perform the write operation

	atomic.StoreInt32(&m.writing, 0) // Atomic write to reset writing flag
	m.writerEnded <- struct{}{}      // Signal that the write operation has ended
}

// StartRWMonitor starts the RWMonitor that manages read and write requests
func (m *RWMonitor[T]) StartRWMonitor() {
	for {
		select {
		case msg := <-m.ReqsChan:
			switch msg := msg.(type) {
			case ReadMessage[T]:
				if atomic.LoadInt32(&m.writing) == 1 || m.reqsQueue.Len() > 0 { // If there is write in progress or queue is not empty
					m.reqsQueue.PushBack(msg) // Add the read request to the queue
				} else { // If no write is in progress and queue is empty, read the data
					m.readersCountMutex.Lock()
					m.readersCount++ // Increment the readers count
					m.readersCountMutex.Unlock()
					go m.read(msg.Res) // Perform the read operation
				}
			case WriteMessage[T]:
				m.readersCountMutex.Lock()
				if m.readersCount > 0 || atomic.LoadInt32(&m.writing) == 1 || m.reqsQueue.Len() > 0 { // If there are readers, write in progress or queue is not empty
					m.reqsQueue.PushBack(msg) // Add the write request to the queue
				} else { // If no write is in progress and queue is empty, perform the write
					atomic.StoreInt32(&m.writing, 1) // Atomic write to set writing flag
					go m.write(msg.Data)             // Perform the write operation
				}
				m.readersCountMutex.Unlock()
			}

		case <-m.readersEnded:
			m.readersCountMutex.Lock()
			if m.readersCount > 0 { // Case when last reader ended while RWMonitor was starting new reader
				m.readersCountMutex.Unlock()
				break // Exit without doing anything
			}

			m.readersCountMutex.Unlock()

			if m.reqsQueue.Len() > 0 { // In this case the only option is write request
				req := m.reqsQueue.Front()         // Get the first request from the queue
				msg := req.Value.(WriteMessage[T]) // Get the write message from the queue
				m.reqsQueue.Remove(req)            // Remove the request from the queue
				atomic.StoreInt32(&m.writing, 1)   // Atomic write to set writing flag
				go m.write(msg.Data)               // Perform the write operation
			}

		case <-m.writerEnded:
		loop:
			for m.reqsQueue.Len() > 0 {
				req := m.reqsQueue.Front() // Get the first request from the queue
				m.reqsQueue.Remove(req)    // Remove the request from the queue

				switch msg := req.Value.(type) {
				case ReadMessage[T]:
					m.readersCountMutex.Lock()
					m.readersCount++ // Increment the readers count
					m.readersCountMutex.Unlock()
					go m.read(msg.Res) // Send the read result

				case WriteMessage[T]:
					m.readersCountMutex.Lock()
					if m.readersCount > 0 { // If the writer ended just before the next reader started
						m.reqsQueue.PushFront(msg) // If there are readers, put the write request back to the front of the queue
						m.readersCountMutex.Unlock()
						break loop // Exit the loop to wait for the next request
					}
					m.readersCountMutex.Unlock()
					atomic.StoreInt32(&m.writing, 1) // Atomic write to set writing flag
					go m.write(msg.Data)             // Perform the write operation
					break loop                       // Exit the loop after processing the write request
				}
			}

		case _, ok := <-m.StopRWMonitor:
			if !ok {
				return // Stop the RWMonitor
			}
		}
	}
}
