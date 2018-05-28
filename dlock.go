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
	DefautSessionTTL = 5 * time.Minute
)

// Dlock configured for lock acquisition
type Dlock struct {
	ConsulClient      *api.Client
	Key               string
	SessionID         string
	LockRetryInterval time.Duration
	SessionTTL        time.Duration
	PermanentRelease  bool
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

// New returns a new Dlock object
func New(o *Config) (*Dlock, error) {
	var d Dlock
	consulClient, err := api.NewClient(api.DefaultConfig())
	if err != nil {
		logger.Println("error on creating consul client", err)
		return &d, err
	}

	d.ConsulClient = consulClient
	d.Key = o.ConsulKey
	d.LockRetryInterval = DefaultLockRetryInterval
	d.SessionTTL = DefautSessionTTL

	if o.LockRetryInterval != 0 {
		d.LockRetryInterval = o.LockRetryInterval
	}
	if o.SessionTTL != 0 {
		d.SessionTTL = o.SessionTTL
	}

	return &d, nil
}

// RetryLockAcquire attempts to acquire the lock at `LockRetryInterval`
// First consul session is created and then attempt is done to acquire lock on this session
// Checks configured over Session is all the checks configured for the client itself
// sends msg to chan `acquired` once lock is acquired
// msg is sent to `released` chan when the lock is released due to consul session invalidation
func (d *Dlock) RetryLockAcquire(value map[string]string, acquired chan<- bool, released chan<- bool) {
	if d.PermanentRelease {
		logger.Printf("lock is permanently released. last session id - %+s", d.SessionID)
		return
	}
	ticker := time.NewTicker(d.LockRetryInterval)
	for ; true; <-ticker.C {
		value["lockAcquisitionTime"] = time.Now().Format(time.RFC3339)
		lock, err := d.acquireLock(value, released)
		if err != nil {
			logger.Println("error on acquireLock :", err, "retry in -", d.LockRetryInterval)
			continue
		}
		if lock {
			logger.Printf("lock acquired with consul session - %s", d.SessionID)
			ticker.Stop()
			acquired <- true
			break
		}
	}
}

// DestroySession invalidates the consul session and indirectly release the acquired lock if any
// Should be called in destructor function e.g clean-up, service reload
// this will give others a chance to acquire lock
func (d *Dlock) DestroySession() error {
	if d.SessionID == "" {
		logger.Printf("cannot destroy empty session")
		return nil
	}
	_, err := d.ConsulClient.Session().Destroy(d.SessionID, nil)
	if err != nil {
		return err
	}
	logger.Printf("destroyed consul session - %s", d.SessionID)
	d.PermanentRelease = true
	return nil
}

func (d *Dlock) createSession() (string, error) {
	return createSession(d.ConsulClient, d.Key, d.SessionTTL)
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

func (d *Dlock) recreateSession() error {
	sessionID, err := d.createSession()
	if err != nil {
		return err
	}
	d.SessionID = sessionID
	return nil
}

func (d *Dlock) acquireLock(value map[string]string, released chan<- bool) (bool, error) {
	if d.SessionID == "" {
		err := d.recreateSession()
		if err != nil {
			return false, err
		}
	}
	b, err := json.Marshal(value)
	if err != nil {
		logger.Println("error on value marshal", err)
	}
	lock, err := d.ConsulClient.LockOpts(&api.LockOptions{Key: d.Key, Value: b, Session: d.SessionID, LockWaitTime: 1 * time.Second, LockTryOnce: true})
	if err != nil {
		return false, err
	}
	a, _, err := d.ConsulClient.Session().Info(d.SessionID, nil)
	if err == nil && a == nil {
		logger.Printf("consul session - %s is invalid now", d.SessionID)
		d.SessionID = ""
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
		doneCh := make(chan struct{})
		go func() { d.ConsulClient.Session().RenewPeriodic(d.SessionTTL.String(), d.SessionID, nil, doneCh) }()
		go func() {
			<-resp
			logger.Printf("lock released with session - %s", d.SessionID)
			close(doneCh)
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
