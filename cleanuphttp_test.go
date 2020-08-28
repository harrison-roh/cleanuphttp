package cleanuphttp

import (
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"
)

func TestDefaultCleanupHTTP(t *testing.T) {
	http.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "Hello world!")
	})

	server := &http.Server{Addr: ":10000"}

	PreCleanupPush(cleanup1, 2)
	PreCleanupPush(cleanup2, 1)
	PostCleanupPush(cleanup3, 4)
	PostCleanupPush(cleanup4, 3)
	Serve(server, 5*time.Second)
}

func TestCleanupHTTP(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		fmt.Fprintf(w, "Hello world!")
	})

	server := &http.Server{
		Addr:    ":10001",
		Handler: mux,
	}

	c := &CleanupHTTP{
		Server: server,
	}

	c.PreCleanupPush(cleanup1, 2)
	c.PreCleanupPush(cleanup2, 1)
	c.PostCleanupPush(cleanup3, 4)
	c.PostCleanupPush(cleanup4, 3)
	c.Serve(5 * time.Second)
}

func cleanup1(x interface{}) {
	log.Printf("clean-up 1 with %d\n", x.(int))
}

func cleanup2(x interface{}) {
	log.Printf("clean-up 2 with %d\n", x.(int))
}

func cleanup3(x interface{}) {
	log.Printf("clean-up 3 with %d\n", x.(int))
}

func cleanup4(x interface{}) {
	log.Printf("clean-up 4 with %d\n", x.(int))
}
