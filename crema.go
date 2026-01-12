package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"

	"github.com/Masterminds/sprig/v3"
	"github.com/apex/log"
	"github.com/apex/log/handlers/text"
	"github.com/clonkspot/gocrema/eventsource"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/render"
)

// GameEventsURL is the URL to the league event stream.
var GameEventsURL = "https://league.clonkspot.org/game_events"

// LeagueURL is the URL to the league server.
var LeagueURL = "https://league.clonkspot.org/league.php"
// LeagueURL is the URL to the league server for the Clonk client.
var LeagueURLClonk = "league.clonkspot.org:80"

func getGameAddresses(id int) ([]net.Addr, error) {
	url := fmt.Sprintf("%s?action=query&game_id=%d", LeagueURL, id)
	res, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	body, err := ioutil.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return nil, err
	}
	linere := regexp.MustCompile(`(?m)^Address=(.+)$`)
	addressre := regexp.MustCompile(`(TCP|UDP):"?([0-9a-f:.[\]]+)"?`)
	m := linere.FindSubmatch(body)
	if m == nil {
		return nil, fmt.Errorf("No Address= line in league answer")
	}
	addrm := addressre.FindAllSubmatch(m[1], -1)
	addrs := make([]net.Addr, len(addrm))
	for i, addr := range addrm {
		switch string(addr[1]) {
		case "TCP":
			addrs[i], err = net.ResolveTCPAddr("tcp", string(addr[2]))
		case "UDP":
			addrs[i], err = net.ResolveUDPAddr("udp", string(addr[2]))
		default:
			err = fmt.Errorf("unexpected network %s", addr[1])
		}
		if err != nil {
			return nil, err
		}
	}

	// Netpuncher info
	npaddrre := regexp.MustCompile(`(?m)^NetpuncherAddr="([^"]+)"$`)
	m = npaddrre.FindSubmatch(body)
	if m != nil {
		netpuncherAddr := string(m[1])
		// Just match the IPv4/IPv6 lines, don't bother with the exact format
		npre := regexp.MustCompile(`(?m)^ +IPv([46])=([0-9]+)$`)
		ms := npre.FindAllSubmatch(body, -1)
		for _, m := range ms {
			id, _ := strconv.ParseUint(string(m[2]), 10, 64)
			addrs = append(addrs, &NetpuncherAddr{Net: "netpuncher" + string(m[1]), Addr: netpuncherAddr, ID: id})
		}
	}

	return addrs, nil
}

func main() {
	log.SetHandler(text.Default)
	//log.SetLevel(log.DebugLevel)

	cache := NewCache()

	go monitorGames(cache)

	r := gin.Default()
	funcmap := sprig.FuncMap()
	funcmap["OverallStatus"] = func(g CacheItem) ConnectStatus {
		s := ConnectStatusFailure
		for _, addr := range g.Addrs {
			switch addr.Status {
			case ConnectStatusSuccess:
				return ConnectStatusSuccess
			case ConnectStatusPending:
				s = ConnectStatusPending
			}
		}
		return s
	}
	funcmap["StatusToString"] = func(s ConnectStatus, success, pending, failure string) (string, error) {
		switch s {
		case ConnectStatusSuccess:
			return success, nil
		case ConnectStatusPending:
			return pending, nil
		case ConnectStatusFailure:
			return failure, nil
		}
		return "", fmt.Errorf("StatusToString: unknown status %d", s)
	}
	markupre := regexp.MustCompile(`<c [0-9a-f]{6}>|<\/c>|<\/?i>`)
	funcmap["RemoveMarkup"] = func(s string) string {
		return markupre.ReplaceAllString(s, "")
	}
	r.SetFuncMap(funcmap)
	r.LoadHTMLGlob("templates/*")
	r.GET("/", func(c *gin.Context) {
		games := cache.Get()
		c.HTML(http.StatusOK, "layout.html", gin.H{
			"Games":     games,
			"LeagueURL": LeagueURLClonk,
		})
	})
	renderRow := func(id int, g *CacheItem) string {
		// this kind of sucks
		html := r.HTMLRender.Instance("gamerow.html", gin.H{
			"ID":        g.Game.ID,
			"G":         g,
			"LeagueURL": LeagueURLClonk,
		}).(render.HTML)

		var output bytes.Buffer
		if err := html.Template.ExecuteTemplate(&output, html.Name, html.Data); err != nil {
			log.WithError(err).Error("rendering row template failed")
			return ""
		}
		return output.String()
	}
	r.GET("/updates", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		updates := cache.GameUpdates.Register()
		defer cache.GameUpdates.Unregister(updates)

		// init: send update event for all games and init event with existing ids
		games := cache.Get()
		ids := make([]int, 0, len(games))
		for id, g := range games {
			ids = append(ids, id)
			c.SSEvent("update", gin.H{
				"id":   id,
				"html": renderRow(id, &g),
			})
		}
		c.SSEvent("init", gin.H{"ids": ids})
		if f, ok := c.Writer.(http.Flusher); ok {
			f.Flush()
		}

		for update := range updates {
			u := update.(*CacheUpdate)
			if u.G != nil {
				c.SSEvent("update", gin.H{
					"id":   u.ID,
					"html": renderRow(u.ID, u.G),
				})
			} else {
				c.SSEvent("delete", gin.H{"id": u.ID})
			}
			if f, ok := c.Writer.(http.Flusher); ok {
				f.Flush()
			}
		}
	})

	// Initialize variable with default value
	var listenAddress string
	if os.Getenv("ADDRESS") != "" {
		listenAddress = os.Getenv("ADDRESS")
	} else {
		listenAddress = "127.0.0.1:8080"
		log.Warn("Environment variable \"ADDRESS\" not set. Using default address 127.0.0.1:8080")
	}
	r.Run(listenAddress)
}

func monitorGames(c *Cache) {
	es := eventsource.New(GameEventsURL)
	defer es.Close()

	for {
		select {
		case <-es.OnOpen:
			// do nothing
		case msg := <-es.OnMessage:
			switch msg.EventType {
			case "init":
				var games []LeagueGame
				if err := json.Unmarshal([]byte(msg.Data), &games); err != nil {
					log.WithError(err).Error("init: error parsing JSON")
					break
				}
				log.Infof("init with %d games\n", len(games))
				c.UpdateAllGames(games)
				for _, game := range games {
					addrs, err := getGameAddresses(game.ID)
					if err != nil {
						log.WithError(err).WithField("id", game.ID).Error("init: error getting addresses")
						continue
					}
					c.UpdateAddrs(game.ID, addrs)
				}
			case "create", "update":
				var game LeagueGame
				if err := json.Unmarshal([]byte(msg.Data), &game); err != nil {
					log.WithError(err).Error("create/update: error parsing JSON")
					break
				}
				c.UpdateGame(game)
				addrs, err := getGameAddresses(game.ID)
				if err != nil {
					log.WithError(err).WithField("id", game.ID).Error("create/update: error getting addresses")
					break
				}
				c.UpdateAddrs(game.ID, addrs)
			case "end", "delete":
				var game LeagueGame
				if err := json.Unmarshal([]byte(msg.Data), &game); err != nil {
					log.WithError(err).Error("end/delete: error parsing JSON")
					break
				}
				c.DeleteGame(game.ID)
			default:
				fmt.Println(msg.EventType, msg.Data)
			}
		case err := <-es.OnError:
			fmt.Printf("err: %v\n", err)
		}
	}
}
