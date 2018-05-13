package dlock

import (
	"encoding/json"
	"log"
	"os"
	"time"

	api "github.com/hashicorp/consul/api"
)

const (
	// DefaultLockRetryInterval is how long we wait after a failed lock acquisition
	DefaultLockRetryInterval = 30 * time.Second
	// DefautSessionTTL is ttl for the session created
	DefautSessionTTL = 60 * 60 * 24 * time.Second
)

// Client provides a client to the dlock API
type Client struct {
	ConsulClient      *api.Client
	Key               string
	SessionID         string
	LockRetryInterval time.Duration
	SessionTTL        time.Duration
}

// Config is used to configure creation of client
type Config struct {
	ConsulKey         string        // key on which lock to acquire
	LockRetryInterval time.Duration // interval at which attempt is done to acquire lock
	SessionTTL        time.Duration // time after which consul session will expire and release the lock
}

var logger *log.Logger

func init() {
	logger = log.New(os.Stdout, "dlock:", log.Ldate|log.Ltime|log.Lshortfile)
}

// SetLogger sets file path for dlock logs
func SetLogger(logpath string) {
	f, err := os.OpenFile(logpath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		log.Printf("error opening file: %v", err)
		return
	}
	logger = log.New(f, "dlock:", log.Ldate|log.Ltime|log.Lshortfile)
}

// NewClient returns a new client
func NewClient(o *Config) (*Client, error) {
	var client Client
	consulClient, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		logger.Println("error on creating consul client", err)
		return &client, err
	}

	client.ConsulClient = consulClient
	client.Key = o.ConsulKey
	client.LockRetryInterval = DefaultLockRetryInterval
	client.SessionTTL = DefautSessionTTL

	if o.LockRetryInterval != 0 {
		client.LockRetryInterval = o.LockRetryInterval
	}
	if o.SessionTTL != 0 {
		client.SessionTTL = o.SessionTTL
	}

	return &client, nil
}

// RetryLockAcquire attempts to acquire the lock at `LockRetryInterval`
// First consul session is created and then attempt is done to acquire lock on this session
// Checks configured over Session is all the checks configured for the client itself
// sends msg to chan `acquired` once lock is acquired
// msg is sent to `released` chan when the lock is released due to consul session invalidation
func (c *Client) RetryLockAcquire(value map[string]string, acquired chan<- bool, released chan<- bool) {
	ticker := time.NewTicker(c.LockRetryInterval)
	for ; true; <-ticker.C {
		value["lockAcquisitionTime"] = time.Now().Format(time.RFC3339)
		lock, err := c.acquireLock(value, released)
		if err != nil {
			logger.Println("error on acquireLock :", err, "retry in -", c.LockRetryInterval)
			continue
		}
		if lock {
			logger.Printf("lock acquired with consul session - %s", c.SessionID)
			ticker.Stop()
			acquired <- true
		}
	}
}

// DestroySession invalidates the consul session and indirectly release the acquired lock if any
// Should be called in destructor function e.g
func (c *Client) DestroySession() error {
	if c.SessionID == "" {
		logger.Printf("cannot destroy empty session")
		return nil
	}
	_, err := c.ConsulClient.Session().Destroy(c.SessionID, nil)
	if err != nil {
		return err
	}
	logger.Printf("destroyed consul session - %s", c.SessionID)
	return nil
}

func (c *Client) createSession() (string, error) {
	return createSession(c.ConsulClient, c.Key, c.SessionTTL)
}

func (c *Client) recreateSession() error {
	sessionID, err := c.createSession()
	if err != nil {
		return err
	}
	c.SessionID = sessionID
	return nil
}

func (c *Client) acquireLock(value map[string]string, released chan<- bool) (bool, error) {
	if c.SessionID == "" {
		err := c.recreateSession()
		if err != nil {
			return false, err
		}
	}
	b, err := json.Marshal(value)
	if err != nil {
		logger.Println("error on value marshal", err)
	}
	lock, err := c.ConsulClient.LockOpts(&api.LockOptions{Key: c.Key, Value: b, Session: c.SessionID, LockWaitTime: 1 * time.Second, LockTryOnce: true})
	if err != nil {
		return false, err
	}
	a, _, err := c.ConsulClient.Session().Info(c.SessionID, nil)
	if err == nil && a == nil {
		logger.Printf("consul session - %s is invalid now", c.SessionID)
		c.SessionID = ""
		return false, nil
	}
	if err != nil {
		return false, err
	}

	resp, err := lock.Lock(nil)
	if err != nil {
		return false, err
	}
	if resp != nil {
		go func() {
			<-resp
			logger.Printf("lock released with session - %s", c.SessionID)
			released <- true
		}()
		return true, nil
	}

	return false, nil
}

func createSession(client *api.Client, consulKey string, ttl time.Duration) (string, error) {
	agentChecks, err := client.Agent().Checks()
	if err != nil {
		logger.Println("error on getting checks", err)
		return "", err
	}
	checks := []string{}
	checks = append(checks, "serfHealth")
	for _, j := range agentChecks {
		checks = append(checks, j.CheckID)
	}

	sessionID, _, err := client.Session().Create(&api.SessionEntry{Name: consulKey, Checks: checks, LockDelay: 0 * time.Second, TTL: ttl.String()}, nil)
	if err != nil {
		return "", err
	}
	logger.Println("created consul session -", sessionID)
	return sessionID, nil
}
