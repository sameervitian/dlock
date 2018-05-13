# dlock [![GoDoc](https://godoc.org/github.com/sameervitian/dlock?status.svg)](https://godoc.org/github.com/sameervitian/dlock)
Distributed lock implementation in Golang using consul 



## Usage

```go 

c, err = dlock.NewClient(&dlock.Config{ConsulKey: "LockKV", LockRetryInterval: time.Second * 10})
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
  go c.RetryLockAcquire(value, acquireCh, releaseCh)
  select {
  case <-acquireCh:
    log.Println("log acquired")
  }
  <-releaseCh // lock is released due to session invalidation
  log.Println("log released")
}
```
