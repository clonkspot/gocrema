package main

// LeagueGame is a JSON-encoded game as returned by game_events.php
type LeagueGame struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Status      string `json:"status"`
	Type        string `json:"type"`
	Comment     string `json:"comment"`
	MaxPlayers  int    `json:"maxPlayers"`
	Host        string `json:"host"`
	Created     string `json:"created"`
	Updated     string `json:"updated"`
	Engine      string `json:"engine"`
	EngineBuild string `json:"engineBuild"`
	Flags       struct {
		JoinAllowed    bool `json:"joinAllowed"`
		PasswordNeeded bool `json:"passwordNeeded"`
	} `json:"flags"`
	Scenario struct {
		FileSize    int    `json:"fileSize"`
		FileCRC     int    `json:"fileCRC"`
		ContentsCRC int    `json:"contentsCRC"`
		Filename    string `json:"filename"`
		Author      string `json:"author"`
	} `json:"scenario"`
	Players []struct {
		Name  string `json:"name"`
		Team  int    `json:"team"`
		Color int    `json:"color"`
	} `json:"players"`
}
