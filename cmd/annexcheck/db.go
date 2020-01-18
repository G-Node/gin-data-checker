package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Simplified User and Repository data representation
//
// Database consists of two maps: One for Users and one for Repositories
// Repositories are indexed by their unique name instead of their ID
// A repository's unique name is lower(owner/reponame)
//
// A user also holds a list of repositories they own.  The full structure is as follows:
//
// gindb
//    |
//    |---> [ID]User (dbuser)
//    |             |
//    |             |--> []Repository (dbrepo)
//    |
//    |---> [owner/name]Repository (dbrepo)

type dbrepo struct {
	ID        int
	OwnerID   int
	Name      string
	IsFork    bool
	OwnerName string
}

type dbuser struct {
	ID           int
	Name         string
	FullName     string
	Email        string
	Repositories []*dbrepo
}

type gindb struct {
	Users        map[int]*dbuser
	Repositories map[string]*dbrepo
}

func newdb() *gindb {
	db := gindb{}
	db.Users = make(map[int]*dbuser)
	db.Repositories = make(map[string]*dbrepo)
	return &db
}

func loaduserdb(path string, db *gindb) {

	fp, err := os.Open(path)
	if err != nil {
		checkerr(err)
	}
	defer fp.Close()

	fmt.Print("Reading user database... ")

	scanner := bufio.NewScanner(fp)
	linenum := 0
	for scanner.Scan() {
		u := dbuser{}
		// Check provided email address against each line as suffix
		jerr := json.Unmarshal(scanner.Bytes(), &u)
		if jerr != nil {
			log.Printf("Failed to read record at line %d", linenum)
		} else {
			db.Users[u.ID] = &u
		}
		linenum++
	}
	fmt.Printf("loaded %d records\n", len(db.Users))
}

func loadrepodb(path string, db *gindb) {
	fp, err := os.Open(path)
	if err != nil {
		checkerr(err)
	}
	defer fp.Close()

	fmt.Print("Reading repository database... ")

	scanner := bufio.NewScanner(fp)
	linenum := 0
	for scanner.Scan() {
		r := dbrepo{}
		// Check provided email address against each line as suffix
		jerr := json.Unmarshal(scanner.Bytes(), &r)
		if jerr != nil {
			log.Printf("Failed to read record at line %d", linenum)
		} else {
			owner, ok := db.Users[r.OwnerID]
			if !ok {
				log.Printf("Repository %q appears to be an orphan", r.Name)
				r.OwnerName = "<ORPHAN>"
			} else {
				r.OwnerName = owner.Name
			}
			db.Repositories[strings.ToLower(filepath.Join(r.OwnerName, r.Name))] = &r
		}
		linenum++
	}
	fmt.Printf("loaded %d records\n", len(db.Repositories))

}

func loaddb(dbloc string) *gindb {
	users := filepath.Join(dbloc, "User.json")
	repositories := filepath.Join(dbloc, "Repository.json")
	db := newdb()
	loaduserdb(users, db)
	loadrepodb(repositories, db)

	return db
}
