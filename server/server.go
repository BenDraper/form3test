package server

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	timeoutResponse  = "RESPONSE|REJECTED|Cancelled"
	acceptedResponse = "RESPONSE|ACCEPTED|Transaction processed"
)

type Server struct {
	listener    net.Listener
	cancelCtx   context.Context
	cancel      context.CancelFunc
	gracePeriod time.Duration
}

func NewServer(address string, gracePeriod time.Duration) (*Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Server{
		listener:    listener,
		cancelCtx:   ctx,
		cancel:      cancel,
		gracePeriod: gracePeriod,
	}, nil
}

func (s *Server) Serve() {
	fmt.Println("starting server")
	for {
		conn, err := s.listener.Accept()
		fmt.Println("accepting connection")
		if err != nil {
			select {
			case <-s.cancelCtx.Done():
				return
			default:
				fmt.Println("Error accepting connection:", err)
				continue
			}
		}

		go s.handleConnection(conn)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Println("Error closing connection:", err)
		}
	}()
	defer println("connection closed")

	scanner := bufio.NewScanner(conn)
	connCtx, connCancel := context.WithCancel(s.cancelCtx)
	defer connCancel()

	for {
		select {
		case <-s.cancelCtx.Done():
			connCancel()
			if _, err := fmt.Fprintf(conn, "%s\n", timeoutResponse); err != nil {
				fmt.Println("Error writing response:", err)
			}
			return
		default:
			if !scanner.Scan() {
				return
			}
			request := scanner.Text()
			if err := scanner.Err(); err != nil {
				fmt.Println("Error reading from connection:", err)
				return
			}

			response := s.handleRequest(connCtx, request)
			if _, err := fmt.Fprintf(conn, "%s\n", response); err != nil {
				fmt.Println("Error writing response:", err)
			}
		}
	}

}

func (s *Server) handleRequest(ctx context.Context, request string) string {
	fmt.Printf("Handling Request: %s\n", request)

	parts := strings.Split(request, "|")
	if len(parts) != 2 || parts[0] != "PAYMENT" {
		return "RESPONSE|REJECTED|Invalid request"
	}

	amount, err := strconv.Atoi(parts[1])
	if err != nil {
		return "RESPONSE|REJECTED|Invalid amount"
	}

	if amount > 100 {
		processingTime := amount
		if amount > 10000 {
			processingTime = 10000
		}
		return longRunningProcess(ctx, processingTime)

	}

	return acceptedResponse
}

func longRunningProcess(ctx context.Context, processingTime int) string {
	select {
	case <-ctx.Done():
		return "RESPONSE|REJECTED|Cancelled"
	case <-time.After(time.Duration(processingTime) * time.Millisecond):
		return acceptedResponse
	}

}

func (s *Server) Shutdown() {
	fmt.Println("Shutting down...")
	s.cancel()
	if err := s.listener.Close(); err != nil {
		fmt.Println("Error closing listener:", err)
	}

	graceTimer := time.NewTimer(s.gracePeriod)
	defer graceTimer.Stop()

	<-graceTimer.C
	fmt.Println("Grace period over. Terminating remaining requests")
}
