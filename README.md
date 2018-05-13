# dlock [![GoDoc](https://godoc.org/github.com/sameervitian/dlock?status.svg)](https://godoc.org/github.com/sameervitian/dlock)
Distributed lock implementation in Golang using consul 



## Usage

##### Dlock Initialization

```go 

d, err = dlock.New(&dlock.Config{ConsulKey: "LockKV", LockRetryInterval: time.Second * 10})
if err != nil {
  log.Println("Error ", err)
  return
}

```

##### Attempt to Acquire Lock 

```go 

acquireCh := make(chan bool)
releaseCh := make(chan bool)

for { // loop is to re-attempt for lock acquisition when the lock was initially acquired but auto released after some time

  log.Println("try to acquire lock")
  value := map[string]string{
    "key1": "val1",
    // Optional keys
    // Any number of similar keys can be added with the limit of 512KB. as mentioned here - https://www.consul.io/docs/faq.html#q-what-is-the-per-key-value-size-limitation-for-consul-39-s-key-value-store-
    // key named `lockAcquisitionTime` is automatically added. This is the time at which lock is acquired. time is in RFC3339 format
  }
  go d.RetryLockAcquire(value, acquireCh, releaseCh) // It will keep on attempting for the lock. The re-attempt interval is configured through `LockRetryInterval`, which is set while dlock initialization. 
  select {
  case <-acquireCh:
    log.Println("log acquired")
  }
  <-releaseCh // lock is released due to session invalidation
  log.Println("log released")
}
```

`acquireCh` recieves msg when the lock is acquired, otherwise blocks and wait for lock acquisition and compete with others for the lock 

`releaseCh` recieves msg when the lock which was earlier acquired is released due to some reason(consul session invalidation etc)

##### Destroy Consul Session and Release lock

```go 

if err := d.DestroySession(); err != nil { // Should be called during clean-up. eg reloading the service. Can be done by catching SIGHUP signal 
//Destroy session will release the lock and give others a chance to acquire the lock
  log.Println(err)
}

```

## Authors

* [Sameer Akhtar](https://github.com/sameervitian)
* [Rishi Bhardwaj](https://github.com/rishitoko)
