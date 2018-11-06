package raft

import (
	"bytes"
	"encoding/gob"
	"project_01/rpc-realize"
	"sync"
	"time"
)

const (
	STATE_LEADER = iota
	STATE_CANDIDATE
	STATE_FOLLOWER

	HBINTERVAL = 50 * time.Millisecond // 心跳检测间隔50ms
)

// 每个raft节点成功提交了日志条目,节点应该返回一个'消息'给服务器,并通过消息通道进行通信
type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool
	Snapshot    []byte
}

type LogEntry struct {
	LogIndex int         //日志条目索引
	LogTerm  int         //任期号
	LogComd  interface{} //命令
}

//
// go 实现单raft节点
//
type Raft struct {
	mu        sync.Mutex
	peers     []*labrpc.ClientEnd //客户端节点
	persister *Persister
	me        int //当前节点在所有客户端节点中的索引位置 index into peers[]

	//
	// 根据Raft论文的论述, 一个Raft服务器必须保证一下的各个特性
	//

	// channel
	state         int
	voteCount     int           // 投票统计
	chanCommit    chan bool     // 提交日志条目
	chanHeartbeat chan bool     // 心跳检测
	chanGrantVote chan bool     //承认投票
	chanLeader    chan bool     // 领导者
	chanApply     chan ApplyMsg //消息通道

	// 所有服务器上都持久存在的状态
	currentTerm int        // 服务器最后一次知道的任期号
	votedFor    int        // 在当前获得选票的候选人的id
	log         []LogEntry // 日志条目集

	//所有服务器经常变的状态
	commitIndex int // 已知的最大的已经被提交的日志条目的索引值
	lastApplied int // 最后被应用到状态机的日志条目索引值

	// 在领导者中经常变的状态
	nextIndex  []int // 对于每一个服务器, 需要发送给他的下一个日志条目的索引值
	matchIndex []int // 对于每一个服务器, 已经复制给他的日志最高索引值
}

func (rf *Raft) readPersist(data []byte) {
	// 对快照进行操作代码
	// 例如：
	r := bytes.NewBuffer(data)
	d := gob.NewDecoder(r)
	d.Decode(&rf.currentTerm)
	d.Decode(&rf.votedFor)
	d.Decode(&rf.log)

}

// 当一个raft节点不在需要的时候， 调用Kill()
func (rf *Raft) Kill() {
	//如果需要杀死raft节点
}

func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// 初始化数据
	rf.state = STATE_FOLLOWER
	rf.votedFor = -1
	rf.log = append(rf.log, LogEntry{LogTerm: 0})
	rf.currentTerm = 0
	rf.chanCommit = make(chan bool, 100)
	rf.chanHeartbeat = make(chan bool, 100)
	rf.chanGrantVote = make(chan bool, 100)
	rf.chanLeader = make(chan bool, 100)
	rf.chanApply = applyCh

	// 崩溃之前从 从快照中初始化
	rf.readPersist(persister.ReadRaftState())
	rf.readSnapshot(persister.ReadSnapshot())
}
