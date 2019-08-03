package main

import "flag"

type config struct {
	// Repostore to scan (recursively)
	Repostore string
	// Database to use for checking forks
	Database string
	// Number of concurrent workers
	NWorkers uint
}

// readargs parses command line arguments and sets up the configuration.
func readargs() config {
	var db string
	var verarg bool
	var nw uint
	flag.StringVar(&db, "database", "", "database to use for determining forks; if unspecified, no fork detection is performed")
	flag.UintVar(&nw, "nworkers", 4, "number of concurrent workers")
	flag.BoolVar(&verarg, "version", false, "show version information")
	flag.Usage = printusage

	flag.Parse()

	if verarg {
		printversion()
	}

	if flag.NArg() > 1 {
		flag.Usage()
	}

	repostore := flag.Arg(0)
	return config{Repostore: repostore, Database: db, NWorkers: uint(nw)}
}
