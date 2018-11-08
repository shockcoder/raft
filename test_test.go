package raft

//
// Raft tests
// test_test.go 来为代码进行测试和评估
//

import (
	"fmt"
	"testing"
	"time"
)

// 选举超时时间 1s (实际的超时时间远不止这么多)
const RaftElectionTimeout = 1000 * time.Millisecond

func TestInitialElection(t *testing.T) {
	// return
	servers := 5
	cfg := make_config(t, servers, false)
	defer cfg.cleanup()

	fmt.Printf("Test: initial election ...\n")

	// is a leader elected?
	cfg.checkOneLeader()

	term1 := cfg.checkTerms()
	time.Sleep(2 * RaftElectionTimeout)
	term2 := cfg.checkTerms()
	if term1 != term2 {
		fmt.Printf("warning: term changed even thought there were on failures")
	}

	fmt.Printf(" ... Passed\n")
}
