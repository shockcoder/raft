package raft

//
// Raft tests
// test_test.go 来为代码进行测试和评估
//

import (
	"testing"
	"time"
)

// 选举超时时间 1s (实际的超时时间远不止这么多)
const RaftElectionTimeout = 1000 * time.Millisecond

func TestInitialElection(t *testing.T) {
	// return
	servers := 5
	cfg := make_config(t, servers, false)
}
