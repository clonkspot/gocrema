package main

import (
	"fmt"
	"net"
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

// Cache is responsible for storing connection tests.
type Cache struct {
	games             map[int]CacheItem
	updateRequestChan chan cacheReq
	checkResultChan   chan cacheCheckMsg
	requestGamesChan  chan chan map[int]CacheItem
}

// NewCache creates a new cache.
func NewCache() *Cache {
	c := &Cache{
		games:             make(map[int]CacheItem),
		updateRequestChan: make(chan cacheReq),
		checkResultChan:   make(chan cacheCheckMsg),
		requestGamesChan:  make(chan chan map[int]CacheItem),
	}
	go c.run()
	return c
}

// UpdateAllGames inserts and updates the given games, deleting all others from
// the cache.
func (c *Cache) UpdateAllGames(games []LeagueGame) {
	c.updateRequestChan <- cacheReq{
		reqType: reqUpdateAll,
		payload: games,
	}
}

// UpdateGame inserts or updates a single game.
func (c *Cache) UpdateGame(game LeagueGame) {
	c.updateRequestChan <- cacheReq{
		reqType: reqUpdateSingle,
		id:      game.ID,
		payload: game,
	}
}

// UpdateAddrs updates a game's addresses.
func (c *Cache) UpdateAddrs(id int, addrs []net.Addr) {
	c.updateRequestChan <- cacheReq{
		reqType: reqUpdateAddrs,
		id:      id,
		payload: addrs,
	}
}

// DeleteGame removes a game from the cache.
func (c *Cache) DeleteGame(id int) {
	c.updateRequestChan <- cacheReq{
		reqType: reqDelete,
		id:      id,
	}
}

// Get retrieves a copy of the currently-cached games.
func (c *Cache) Get() map[int]CacheItem {
	res := make(chan map[int]CacheItem)
	c.requestGamesChan <- res
	return <-res
}

// internal (run): copyState copies the cache state.
func (c *Cache) copyState() map[int]CacheItem {
	games := make(map[int]CacheItem)
	for id, game := range c.games {
		g := game
		g.Addrs = make(map[string]CacheItemAddr)
		for key, addr := range game.Addrs {
			g.Addrs[key] = addr
		}
		games[id] = g
	}
	return games
}

// run starts the cache main loop.
func (c *Cache) run() {
	updateGame := func(game *LeagueGame) {
		g, ok := c.games[game.ID]
		if !ok {
			g = CacheItem{Addrs: make(map[string]CacheItemAddr)}
		}
		g.Game = *game
		c.games[game.ID] = g
	}
	for {
		select {
		case req := <-c.updateRequestChan:
			switch req.reqType {
			case reqUpdateAll:
				games := req.payload.([]LeagueGame)
				seen := make(map[int]bool)
				for _, game := range games {
					updateGame(&game)
					seen[game.ID] = true
				}
				// delete games that weren't updated
				for id := range c.games {
					if !seen[id] {
						delete(c.games, id)
					}
				}
			case reqUpdateSingle:
				game := req.payload.(LeagueGame)
				updateGame(&game)
			case reqUpdateAddrs:
				// drop request for unknown games
				if game, ok := c.games[req.id]; ok {
					addrs := req.payload.([]net.Addr)
					for _, addr := range addrs {
						if !shouldSkipAddr(addr) {
							if _, ok := game.Addrs[cacheAddrKey(addr)]; !ok {
								// item is not in cache, check it now
								game.Addrs[cacheAddrKey(addr)] = CacheItemAddr{Addr: addr, Status: ConnectStatusPending}
								go c.check(cacheCheckMsg{id: req.id, addr: addr})
							}
						}
					}
				}
			case reqDelete:
				delete(c.games, req.id)
			}
		case res := <-c.checkResultChan:
			if game, ok := c.games[res.id]; ok {
				key := cacheAddrKey(res.addr)
				a := game.Addrs[key]
				a.Status = res.status
				game.Addrs[key] = a
			}
		case resChan := <-c.requestGamesChan:
			resChan <- c.copyState()
		}
	}
}

type cacheCheckMsg struct {
	id     int           // game id
	addr   net.Addr      // address to check
	status ConnectStatus // reply: status
}

// check tries to connect to the given address. Should be run from a goroutine.
func (c *Cache) check(req cacheCheckMsg) {
	req.status = ConnectStatusFailure
	if tryConnect(req.addr) {
		req.status = ConnectStatusSuccess
	}
	c.checkResultChan <- req
}

type cacheReqType int

const (
	reqUpdateAll cacheReqType = iota
	reqUpdateSingle
	reqUpdateAddrs
	reqDelete
)

type cacheReq struct {
	reqType cacheReqType
	id      int
	payload interface{}
}

// CacheItem is a game with associated addresses.
type CacheItem struct {
	Game  LeagueGame               // includes ID
	Addrs map[string]CacheItemAddr // indexed by cacheAddrKey
}

func cacheAddrKey(a net.Addr) string {
	return fmt.Sprintf("%s:%s", a.Network(), a.String())
}

// CacheItemAddr is a single address that has been checked.
type CacheItemAddr struct {
	Addr   net.Addr
	Status ConnectStatus
}
