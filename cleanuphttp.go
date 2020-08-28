package cleanuphttp

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultCleanupHTTP is the default cleanuphttp.
var DefaultCleanupHTTP = &defaultCleanupHTTP

var defaultCleanupHTTP CleanupHTTP

// Routine is cleanup function type.
type Routine func(interface{})

// CleanupHTTP runs clean-up routines before/after the server is shut down.
type CleanupHTTP struct {
	Server *http.Server

	preRoutines  routineStack
	postRoutines routineStack
	closing      int32
	interrupted  bool
}

// Serve runs the http server with clean-up routines.
func (c *CleanupHTTP) Serve(timeout time.Duration) {
	quit := make(chan struct{})
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGINT, syscall.SIGTERM)

	go c.handleSignal(interrupt, quit)

	go func() {
		if err := c.Server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("ListenAndServe failed: %s", err.Error())
		}

		if atomic.CompareAndSwapInt32(&c.closing, 0, 1) {
			close(quit)
		}
	}()

	<-quit
	c.preCleanup()
	defer c.postCleanup()

	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	if timeout > 0 {
		ctx, cancel = context.WithTimeout(
			context.Background(),
			timeout,
		)
		defer cancel()
	} else {
		ctx = context.Background()
	}

	if err := c.Server.Shutdown(ctx); err != nil {
		log.Printf("Shutdown failed: %s", err.Error())
	}
}

// PreCleanupPush pushes routine that runs before the given server is shut down
// onto the top of the stack of clean-up handlers.
// When routine is later invoked, it will be given arg as its argument.
func (c *CleanupHTTP) PreCleanupPush(routine Routine, arg interface{}) {
	c.cleanupPush(&c.preRoutines, routine, arg)
}

// PreCleanupPop removes the routine at the top of the stack
// of clean-up handlers for the given server.
func (c *CleanupHTTP) PreCleanupPop() (Routine, interface{}, bool) {
	return c.cleanupPop(&c.preRoutines)
}

// PostCleanupPush pushes routine that runs after the given server is shut down
// onto the top of the stack of clean-up handlers.
// When routine is later invoked, it will be given arg as its argument.
func (c *CleanupHTTP) PostCleanupPush(routine Routine, arg interface{}) {
	c.cleanupPush(&c.postRoutines, routine, arg)
}

// PostCleanupPop removes the routine at the top of the stack
// of clean-up handlers for the given server.
func (c *CleanupHTTP) PostCleanupPop() (Routine, interface{}, bool) {
	return c.cleanupPop(&c.postRoutines)
}

func (c *CleanupHTTP) cleanupPush(stack *routineStack, routine Routine, arg interface{}) {
	if c.isClosed() {
		log.Println("Push failed: closed")
		return
	}

	r := cleanupRoutine{
		routine: routine,
		arg:     arg,
	}
	stack.push(r)
}

func (c *CleanupHTTP) cleanupPop(stack *routineStack) (Routine, interface{}, bool) {
	if c.isClosed() {
		log.Println("Pop failed: closed")
		return nil, nil, false
	}

	if r, ok := stack.pop(); ok {
		routine := r.routine
		arg := r.arg
		return routine, arg, true
	}

	return nil, nil, false
}

func (c *CleanupHTTP) handleSignal(interrupt chan os.Signal, quit chan struct{}) {
	for i := range interrupt {
		log.Printf("System call: %+v", i)
		if c.interrupted {
			continue
		}
		c.interrupted = true

		if c.isClosed() {
			continue
		}

		if atomic.CompareAndSwapInt32(&c.closing, 0, 1) {
			close(quit)
		}
	}
}

func (c *CleanupHTTP) preCleanup() {
	c.cleanup(&c.preRoutines)
}

func (c *CleanupHTTP) postCleanup() {
	c.cleanup(&c.postRoutines)
}

func (c *CleanupHTTP) cleanup(stack *routineStack) {
	for {
		if r, ok := stack.pop(); ok {
			routine := r.routine
			arg := r.arg
			routine(arg)
		} else {
			break
		}
	}
}

func (c *CleanupHTTP) isClosed() bool {
	return atomic.LoadInt32(&c.closing) != 0
}

// Serve runs the given http server with cleanup routines.
func Serve(server *http.Server, timeout time.Duration) {
	DefaultCleanupHTTP.Server = server
	DefaultCleanupHTTP.Serve(timeout)
}

// PreCleanupPush pushes routine that runs before a server is shut down
// onto the top of the stack of clean-up handlers in the DefaultCleanupHTTP.
// When routine is later invoked, it will be given arg as its argument.
func PreCleanupPush(routine Routine, arg interface{}) {
	DefaultCleanupHTTP.cleanupPush(&DefaultCleanupHTTP.preRoutines, routine, arg)
}

// PreCleanupPop removes the routine at the top of the stack
// of clean-up handlers in the DefaultCleanupHTTP.
func PreCleanupPop() (Routine, interface{}, bool) {
	return DefaultCleanupHTTP.cleanupPop(&DefaultCleanupHTTP.preRoutines)
}

// PostCleanupPush pushes routine that runs before a server is shut down
// onto the top of the stack of clean-up handlers in the DefaultCleanupHTTP.
// When routine is later invoked, it will be given arg as its argument.
func PostCleanupPush(routine Routine, arg interface{}) {
	DefaultCleanupHTTP.cleanupPush(&DefaultCleanupHTTP.postRoutines, routine, arg)
}

// PostCleanupPop removes the routine at the top of the stack
// of clean-up handlers in the DefaultCleanupHTTP.
func PostCleanupPop() (Routine, interface{}, bool) {
	return DefaultCleanupHTTP.cleanupPop(&DefaultCleanupHTTP.postRoutines)
}

type cleanupRoutine struct {
	routine Routine
	arg     interface{}
}

type routineStack struct {
	mu       sync.Mutex
	routines []cleanupRoutine
}

func (s *routineStack) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.routines)
}

func (s *routineStack) push(routine cleanupRoutine) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.routines = append(s.routines, routine)
}

func (s *routineStack) pop() (cleanupRoutine, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.routines) == 0 {
		return cleanupRoutine{}, false
	}

	index := len(s.routines) - 1
	routine := s.routines[index]
	s.routines = s.routines[:index]

	return routine, true
}
