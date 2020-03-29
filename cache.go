package main

import (
	"fmt"
	"net"
	"time"
)

// ConnectStatus is the result of a connection check
type ConnectStatus int

var (
	ConnectStatusPending ConnectStatus     // address has not been checked yet
	ConnectStatusSuccess ConnectStatus = 1 // connection to the address was successful
	ConnectStatusFailure ConnectStatus = 2 // connection to the address has failed
)

func (s ConnectStatus) String() string {
	switch s {
	case ConnectStatusPending:
		return "pending"
	case ConnectStatusSuccess:
		return "success"
	case ConnectStatusFailure:
		return "failure"
	default:
		return "unknown"
	}
}

// cacheDuration is how long an entry stays in the cache.
var cacheDuration = 10 * time.Minute

// Cache is responsible for storing connection tests.
type Cache struct {
	addrs       map[string]*CacheItem
	requestChan chan cacheRequestInternal
	resultChan  chan *CacheItem
}

// NewCache creates a new cache.
func NewCache() *Cache {
	c := &Cache{
		addrs:       make(map[string]*CacheItem),
		requestChan: make(chan cacheRequestInternal),
		resultChan:  make(chan *CacheItem),
	}
	go c.run()
	return c
}

// run starts the cache main loop.
func (c *Cache) run() {
	for {
		// TODO: Expire old cache items
		select {
		case ri := <-c.requestChan:
			if item, ok := c.addrs[ri.req.key()]; ok {
				// return the item
				ri.reply <- item
			} else {
				// item is not in cache, check it now
				c.addrs[ri.req.key()] = &CacheItem{CacheRequest: *ri.req, Status: ConnectStatusPending, expiry: time.Now().Add(cacheDuration)}
				ri.reply <- c.addrs[ri.req.key()]
				go c.check(ri.req)
			}
		case res := <-c.resultChan:
			c.addrs[res.key()] = res
		}
	}
}

// check tries to connect to the given address. Should be run from a goroutine.
func (c *Cache) check(req *CacheRequest) {
	status := ConnectStatusFailure
	if tryConnect(req.Addr) {
		status = ConnectStatusSuccess
	}
	c.resultChan <- &CacheItem{
		CacheRequest: *req,
		Status:       status,
		expiry:       time.Now().Add(cacheDuration),
	}
}

// Get retrieves the status of an address from the cache.
func (c *Cache) Get(req CacheRequest) ConnectStatus {
	reply := make(chan *CacheItem)
	c.requestChan <- cacheRequestInternal{req: &req, reply: reply}
	item := <-reply
	return item.Status
}

// CacheRequest is a request to check a possibly-new address.
type CacheRequest struct {
	ID   int      // ID is the game's id from the league
	Addr net.Addr // Addr is the potential address to connect to
}

// key returns a string key for putting the request in a hash map.
func (req *CacheRequest) key() string {
	return fmt.Sprintf("%d:%s:%s", req.ID, req.Addr.Network(), req.Addr.String())
}

type cacheRequestInternal struct {
	req   *CacheRequest
	reply chan *CacheItem
}

// CacheItem is an address that has been checked.
type CacheItem struct {
	CacheRequest
	Status ConnectStatus
	expiry time.Time
}
