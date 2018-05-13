package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robfig/cron"
	"github.com/sameervitian/dlock"
)

func main() {

	var port = flag.Int64("port", 9001, "port")
	flag.Parse()

	var d *dlock.Dlock
	var err error

	go func() {
		mcron := NewMCron()
		// dlock.SetLogger("/tmp/dlock.log") // If set, dlock logs will be be directed to the file path specified, else on Stdout
		d, err = dlock.New(&dlock.Config{ConsulKey: "LockKV", LockRetryInterval: time.Second * 10})
		if err != nil {
			log.Println("Error ", err)
			return
		}
		acquireCh := make(chan bool)
		releaseCh := make(chan bool)

		for { // loop is to re-attempt for lock acquisition when the lock was initially acquired but auto released after some time
			log.Println("try to acquire lock")
			hostname, _ := os.Hostname()
			value := map[string]string{
				"hostname": hostname,
				// Any number of similar keys can be added
				// key named `lockAcquisitionTime` is automatically added. This is the time at which lock is acquired. time is in RFC3339 format
			}
			go d.RetryLockAcquire(value, acquireCh, releaseCh)
			select {
			case <-acquireCh:
				mcron.Start() // Start the cron when lock is acquired
			}
			<-releaseCh
			mcron.Stop() // Stop the cron when lock is released
		}

	}()

	errCh := make(chan error)
	go func() {
		log.Println("http server running on port", *port)
		errCh <- http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	}()

	term := make(chan os.Signal)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)
	select {
	case <-term:
		if err := d.DestroySession(); err != nil {
			log.Println(err)
		}
		log.Println("Exiting gracefully...")
	case err := <-errCh:
		log.Println("Error starting web server, exiting gracefully:", err)
	}

}

// Cron logic

type MCron struct {
	cron *cron.Cron
}

func NewMCron() *MCron {
	mcron := &MCron{}
	c := cron.New()
	c.AddFunc("*/3 * * * * *", func() { fmt.Println("Every 3 sec") })
	mcron.cron = c
	return mcron
}

func (a *MCron) Start() {
	log.Println("cron started")
	a.cron.Start()
}

func (a *MCron) Stop() {
	log.Println("cron stopped")
	a.cron.Stop()
}
