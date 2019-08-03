package main

import "fmt"

type workerqueue struct {
	queue     chan *repository
	nworkers  uint
	njobs     uint64
	ncomplete uint64
}

func newworkerqueue(nworkers uint, njobs uint64) *workerqueue {
	wq := workerqueue{}
	wq.queue = make(chan *repository, njobs)
	wq.nworkers = nworkers
	wq.njobs = njobs

	return &wq
}

func (wq *workerqueue) start() {
	for idx := uint(0); idx < wq.nworkers; idx++ {
		go wq.startworker()
		fmt.Printf("Worker %d started\n", idx)
	}
}

func (wq *workerqueue) wait() {
	for wq.ncomplete < wq.njobs {
		fmt.Printf(" : %d/%d\r", wq.ncomplete, wq.njobs)
	}
	close(wq.queue)
	fmt.Printf("\n%d jobs complete. Stopping workers.\n", wq.ncomplete)
}

func (wq *workerqueue) startworker() {
	for repo := range wq.queue {
		findMissingAnnex(repo)
		wq.ncomplete++
	}
}

func (wq *workerqueue) submitjob(r *repository) {
	wq.queue <- r
}
