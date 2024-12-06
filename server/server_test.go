package server

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const port = 8080

func TestSchemeSimulator(t *testing.T) {
	tests := []struct {
		name                    string
		input                   string
		expectedOutput          string
		minDuration             time.Duration
		maxDuration             time.Duration
		shutdownDelay           time.Duration
		connectDelay            time.Duration
		requestDelay            time.Duration
		gracePeriod             time.Duration
		expectedConnectionError error
		expectedRequestError    error
	}{
		{
			name:           "Valid Request",
			input:          "PAYMENT|10",
			expectedOutput: "RESPONSE|ACCEPTED|Transaction processed",
			maxDuration:    50 * time.Millisecond,
		},
		{
			name:           "Valid Request with Delay",
			input:          "PAYMENT|101",
			expectedOutput: "RESPONSE|ACCEPTED|Transaction processed",
			minDuration:    101 * time.Millisecond,
			maxDuration:    151 * time.Millisecond,
		},

		{
			name:           "Invalid Request Format",
			input:          "INVALID|100",
			expectedOutput: "RESPONSE|REJECTED|Invalid request",
			maxDuration:    10 * time.Millisecond,
		},
		{
			name:           "Large Amount",
			input:          "PAYMENT|20000",
			expectedOutput: "RESPONSE|ACCEPTED|Transaction processed",
			minDuration:    10 * time.Second,
			maxDuration:    10*time.Second + 50*time.Millisecond,
		},
		{
			name:           "Successful Shutdown",
			input:          "PAYMENT|10",
			expectedOutput: "RESPONSE|ACCEPTED|Transaction processed",
			shutdownDelay:  time.Second,
			gracePeriod:    1 * time.Second,
		},
		{
			name:           "Long Request Terminated During Shutdown",
			input:          "PAYMENT|20000",
			expectedOutput: "RESPONSE|REJECTED|Cancelled",
			shutdownDelay:  100 * time.Millisecond,
			gracePeriod:    2 * time.Second,
		},
		{
			name:                    "Connections sent after shutdown requested are rejected",
			input:                   "PAYMENT|20000",
			expectedOutput:          "RESPONSE|REJECTED|Cancelled",
			shutdownDelay:           1 * time.Millisecond,
			connectDelay:            1000 * time.Millisecond,
			expectedConnectionError: fmt.Errorf("dial tcp :%d: connect: connection refused", port),
			gracePeriod:             3 * time.Second,
		},
		{
			name:           "Requests to existing connections processed - within grace period",
			input:          "PAYMENT|10",
			expectedOutput: "RESPONSE|ACCEPTED|Transaction processed",
			shutdownDelay:  1 * time.Millisecond,
			requestDelay:   100 * time.Millisecond,
			gracePeriod:    1 * time.Second,
		},
		{
			name:           "Requests to existing connections processed - longer than grace period",
			input:          "PAYMENT|20000",
			expectedOutput: "RESPONSE|REJECTED|Cancelled",
			shutdownDelay:  1 * time.Millisecond,
			requestDelay:   100 * time.Millisecond,
			gracePeriod:    5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			server, shutdown := startServer(tt.gracePeriod, tt.shutdownDelay)

			if tt.connectDelay > 0 {
				time.Sleep(tt.connectDelay)
			}

			checks := func(response string, duration time.Duration) {
				assert.Equal(t, tt.expectedOutput, response, "Unexpected response")

				if tt.minDuration > 0 {
					assert.GreaterOrEqual(t, duration, tt.minDuration, "Response time was shorter than expected")
				}

				if tt.maxDuration > 0 {
					assert.LessOrEqual(t, duration, tt.maxDuration, "Response time was longer than expected")
				}

				if tt.shutdownDelay <= 0 {
					server.Shutdown()
				} else {
					<-shutdown
				}
			}

			sendRequests(t, tt.input, 1, tt.requestDelay, tt.expectedConnectionError, tt.expectedRequestError, checks)

		})
	}
}

func startServer(gracePeriod time.Duration, shutdownDelay time.Duration) (*Server, chan struct{}) {

	if gracePeriod <= 0 {
		gracePeriod = 1000 * time.Millisecond
	}

	server, err := NewServer(fmt.Sprintf("localhost:%d", port), gracePeriod)
	if err != nil {
		panic(err)
	}

	shutdown := make(chan struct{}, 1)

	go server.Serve()

	// wait of the server to be ready
	time.Sleep(time.Second)

	if shutdownDelay > 0 {
		go func() {
			time.Sleep(shutdownDelay)
			server.Shutdown()
			close(shutdown)
		}()
	}

	return server, shutdown
}

func sendRequests(t *testing.T, input string, requestCount int, requestDelay time.Duration, expectedConnectionError, expectedRequestError error, checks func(string, time.Duration)) {
	conn, err := net.Dial("tcp", fmt.Sprintf(":%d", port))
	if expectedConnectionError != nil {
		assert.NotNil(t, err)
		assert.Equal(t, expectedConnectionError.Error(), err.Error(), "connecting to server")
		return
	}
	require.NoError(t, err, "Failed to connect to server")
	defer conn.Close()

	if requestDelay > 0 {
		time.Sleep(requestDelay)
	}

	for i := 0; i < requestCount; i++ {
		_, err = fmt.Fprintf(conn, "%s\n", input)
		if expectedRequestError != nil {
			assert.NotNil(t, err)
			assert.Equal(t, expectedRequestError.Error(), err.Error(), "request to server")
			return
		}
		require.NoError(t, err, "Failed to send request")

		start := time.Now()

		response, err := bufio.NewReader(conn).ReadString('\n')
		require.NoError(t, err, "Failed to read response")
		duration := time.Since(start)

		response = strings.TrimSpace(response)

		checks(response, duration)
	}
}

func TestVolume(t *testing.T) {
	tests := []struct {
		name                  string
		input                 string
		expectedOutput        string
		connectionCount       int
		requestsPerConnection int
	}{
		{
			name:                  "500 short requests",
			input:                 "PAYMENT|10",
			expectedOutput:        "RESPONSE|ACCEPTED|Transaction processed",
			connectionCount:       50,
			requestsPerConnection: 10,
		},
		{
			name:                  "1 long requests per 10 connections",
			input:                 "PAYMENT|10000",
			expectedOutput:        "RESPONSE|ACCEPTED|Transaction processed",
			connectionCount:       10,
			requestsPerConnection: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			server, _ := startServer(0, 0)

			checks := func(response string, duration time.Duration) {
				assert.Equal(t, tt.expectedOutput, response, "Unexpected response")
			}

			wg := sync.WaitGroup{}

			for i := 0; i < tt.connectionCount; i++ {
				wg.Add(1)

				go func() {
					defer wg.Done()
					sendRequests(t, tt.input, tt.requestsPerConnection, 0, nil, nil, checks)
				}()
			}

			wg.Wait()
			server.Shutdown()

		})
	}
}
