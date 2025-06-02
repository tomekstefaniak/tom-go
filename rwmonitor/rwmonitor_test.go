package ex4

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"
)

func TestRWMonitor(t *testing.T) {
	// Data that will be read and written that is "not prepared" for race condtions
	vulnreable := 0

	// Simple read function with logs
	readFunc := func() int {
		id := rand.Intn(1_000_000) // Generate a random ID for the reader to identify it on the logs
		fmt.Printf("%x started reading\n", id)

		data := vulnreable                                                      // Read the data from the vulnerable variable
		time.Sleep(time.Duration((rand.Intn(10) + 10) * int(time.Millisecond))) // Simulate some delay

		fmt.Printf("%x finished reading: %x\n", id, data)
		return data                                                             // Return the read data
	}

	// Simple write function
	writeFunc := func(data int) {
		id := rand.Intn(1_000_000) // Generate a random ID for the reader to identify it on the logs
		fmt.Printf(".. %x started writing\n", id)

		vulnreable = id                                                         // Write the data to the vulnerable variable
		time.Sleep(time.Duration((rand.Intn(10) + 10) * int(time.Millisecond))) // Simulate some delay

		fmt.Printf(".. %x finished writing\n", id)
	}

	// Create a monitor with the read and write functions
	monitor := CreateRWMonitor(readFunc, writeFunc, 1000)
	go monitor.StartRWMonitor()

	var wg sync.WaitGroup

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for ttl := 10; ttl > 0; ttl-- {
				if rand.Intn(2) == 0 { // Read operation
					res := make(chan int)
					monitor.ReqsChan <- ReadMessage[int]{Res: res}    // Send a read request
					<-res                                     // Wait for the read result
				} else { // Write operation
					monitor.ReqsChan <- WriteMessage[int]{Data: id} // Send a write request
				}

			}
		}(i)
	}

	wg.Wait()
	// Close the monitor
	close(monitor.StopRWMonitor)
}
