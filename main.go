package main

import (
	"github.com/jinzhu/gorm"
	_ "github.com/mattn/go-sqlite3"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"
)

type Player struct {
	ID    int
	Name  string
	Score int
}

type Match struct {
	ID        int
	CreatedAt time.Time
	P1        Player
	P1ID      int
	Score1    int
	P2        Player
	P2ID      int
	Score2    int
	Confirmed bool
}

type DisplayValues struct {
	Message          string
	Players          []Player
	ConfirmedMatches []Match
	QueuedMatches    []Match
	HistoryLen       int
}

var t *template.Template
var db *gorm.DB
var sig chan os.Signal

func isLeagueStarted() bool {
	count := 0

	if db != nil {
		db.Model(&Match{}).Count(&count)
	}

	return count != 0
}

func getMessageCookie(msg string) *http.Cookie {
	return &http.Cookie{
		Name:   "msg",
		Value:  msg,
		Path:   "/",
		MaxAge: 60,
	}
}

func deleteMessageCookie() *http.Cookie {
	return &http.Cookie{
		Name:   "msg",
		MaxAge: -1,
	}
}

func AddPlayer(w http.ResponseWriter, r *http.Request) {
	var msg string

	if isLeagueStarted() {
		msg = "Non puoi aggiungere un giocatore a campionato iniziato!"
	}

	if r.FormValue("playername") == "" {
		msg = "Nome giocatore non valido!"
	}

	if msg == "" {
		db.Create(&Player{Name: r.FormValue("playername")})

		msg = "Giocatore aggiunto!"
	}

	http.SetCookie(w, getMessageCookie(msg))
	http.Redirect(w, r, "/", http.StatusFound)
}

func AddMatch(w http.ResponseWriter, r *http.Request) {
	var msg string

	P1ID, err := strconv.Atoi(r.FormValue("p1id"))
	if err != nil || P1ID < 0 {
		msg = "Primo giocatore non valido!"
	}

	P2ID, err := strconv.Atoi(r.FormValue("p2id"))
	if err != nil || P2ID < 0 {
		msg = "Secondo giocatore non valido!"
	}

	S1, err := strconv.Atoi(r.FormValue("score1"))
	if err != nil || S1 < 0 {
		msg = "Primo punteggio non valido!"
	}

	S2, err := strconv.Atoi(r.FormValue("score2"))
	if err != nil || S2 < 0 {
		msg = "Secondo punteggio non valido!"
	}

	if P1ID == P2ID {
		msg = "I giocatori non possono sfidare se stessi!"
	}

	if S1 == S2 {
		msg = "Pareggio non ammesso!"
	}

	if msg == "" {
		tx := db.Begin()
		defer func() {
			if err := recover(); err != nil {
				tx.Rollback()
			}
		}()

		match := &Match{P1ID: P1ID, P2ID: P2ID, Score1: S1, Score2: S2}
		tx.Create(match)

		UpdateScores(r, tx)

		msg = "Partita aggiunta!"
	}

	http.SetCookie(w, getMessageCookie(msg))
	http.Redirect(w, r, "/", http.StatusFound)
}

func UpdateScores(r *http.Request, tx *gorm.DB) {
	var players []*Player

	tx.Find(&players)

	matches := make([]*Match, 0, len(players))

	scoresUpdated := true

outer:
	for i, p0 := range players {
		// there are at least 2 players
		for _, p := range players[i+1:] {
			match := &Match{}

			count := 0
			tx.Where("confirmed = ? AND ((p1_id = ? AND p2_id = ?) OR (p1_id = ? AND p2_id = ?))", false, p0.ID, p.ID, p.ID, p0.ID).Order("created_at").First(&match).Count(&count)

			if count == 0 {
				scoresUpdated = false
				break outer
			}

			match.Confirmed = true
			matches = append(matches, match)

			var winnerID, winnerScore, loserScore int
			var winner, loser *Player

			if match.Score1 > match.Score2 {
				winnerID = match.P1ID
				winnerScore = match.Score1
				loserScore = match.Score2
			} else {
				winnerID = match.P2ID
				winnerScore = match.Score2
				loserScore = match.Score1
			}

			if winnerID == p0.ID {
				winner = p0
				loser = p
			} else {
				winner = p
				loser = p0
			}

			winner.Score += winnerScore * 2
			loser.Score += loserScore
		}
	}

	if scoresUpdated {
		for _, p := range players {
			tx.Save(p)
		}

		for _, m := range matches {
			tx.Save(m)
		}
	}

	tx.Commit()
}

func Index(w http.ResponseWriter, r *http.Request) {
	var dv DisplayValues

	if msg, err := r.Cookie("msg"); err == nil {
		dv.Message = msg.Value
		http.SetCookie(w, deleteMessageCookie())
	}

	db.Order("score desc").Find(&dv.Players)
	db.Preload("P1").Preload("P2").Order("created_at desc").Where(&Match{Confirmed: true}).Find(&dv.ConfirmedMatches)
	db.Preload("P1").Preload("P2").Order("created_at desc").Not(&Match{Confirmed: true}).Find(&dv.QueuedMatches)

	t.Execute(w, dv)
}

func signalsHandler() {
	<-sig

	log.Println("shutting down")

	if db != nil {
		db.Close()
	}

	os.Exit(0)
}

func main() {
	var err error

	sig = make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go signalsHandler()

	t, err = template.ParseFiles("index.html")
	if err != nil {
		log.Println(err)
		return
	}

	db, err = gorm.Open("sqlite3", "pingpong.db")
	if err != nil {
		log.Println(err)
		return
	}
	//db.LogMode(true)

	db.AutoMigrate(&Player{}, &Match{})

	http.Handle("/favicon.ico", http.NotFoundHandler())
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("./"))))
	http.HandleFunc("/player/add", AddPlayer)
	http.HandleFunc("/match/add", AddMatch)
	http.HandleFunc("/", Index)
	log.Println(http.ListenAndServe(":http-alt", nil))
}
