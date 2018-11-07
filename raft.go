package raft

import (
	"bytes"
	"encoding/gob"
	"math/rand"
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

func (rf *Raft) getLastIndex() int {
	return rf.log[len(rf.log)-1].LogIndex
}

// 发送日志条目所需要的参数
type AppendEntriesArgs struct {
	Term         int
	LeaderId     int
	PrevLogTerm  int
	PrevLogIndex int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term      int
	Success   bool
	NextIndex int
}

// 保存raft的持久状态到 稳定的存储器
// 以便崩溃后重启
func (rf *Raft) persist() {
	w := new(bytes.Buffer)
	e := gob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	data := w.Bytes()
	rf.persister.SaveRaftState(data)
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

func (rf *Raft) readSnapshot(data []byte) {
	rf.readPersist(rf.persister.ReadRaftState())

	if len(data) == 0 {
		return
	}

	r := bytes.NewBuffer(data)
	d := gob.NewDecoder(r)

	var LastIncludedIndex int
	var LastIncludedTerm int

	d.Decode(&LastIncludedIndex)
	d.Decode(&LastIncludedTerm)

	rf.commitIndex = LastIncludedIndex
	rf.lastApplied = LastIncludedIndex

	rf.log = truncateLog(LastIncludedIndex, LastIncludedTerm, rf.log)

	msg := ApplyMsg{UseSnapshot: true, Snapshot: data}

	go func() {
		rf.chanApply <- msg
	}()

}

func truncateLog(lastIncludeIndex int, lastIncludedTerm int, log []LogEntry) []LogEntry {
	var newLogEntries []LogEntry
	newLogEntries = append(newLogEntries, LogEntry{LogIndex: lastIncludeIndex, LogTerm: lastIncludedTerm})

	for index := len(log) - 1; index >= 0; index-- {
		if log[index].LogIndex == lastIncludeIndex && log[index].LogTerm == lastIncludedTerm {
			newLogEntries = append(newLogEntries, log[index+1:]...)
			break
		}
	}

	return newLogEntries
}

// 当一个raft节点不在需要的时候， 调用Kill()
func (rf *Raft) Kill() {
	//如果需要杀死raft节点
}

type InstallSnapshotArgs struct {
	Term             int
	LeaderId         int
	LastIncludeIndex int
	LastIncludeTerm  int
	Data             []byte
}

type InstallSnapshotReply struct {
	Term int
}

func (rf *Raft) sendInstallSnapshot(server int, args InstallSnapshotArgs, reply *InstallSnapshotReply) bool {
	ok := rf.peers[server].Call("Raft.InstallSnapshot", args, reply)
	if ok {
		if reply.Term > rf.currentTerm {
			rf.currentTerm = reply.Term
			rf.state = STATE_FOLLOWER
			rf.votedFor = -1
			return ok
		}

		rf.nextIndex[server] = args.LastIncludeIndex + 1
		rf.matchIndex[server] = args.LastIncludeIndex
	}

	return ok
}

func (rf *Raft) sendAppendEntries(server int, args AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if ok {
		if rf.state != STATE_LEADER {
			return ok
		}
		if args.Term != rf.currentTerm {
			return ok
		}

		if reply.Term > rf.currentTerm {
			rf.currentTerm = reply.Term
			rf.state = STATE_FOLLOWER
			rf.votedFor = -1
			rf.persist()
			return ok
		}
		if reply.Success {
			if len(args.Entries) > 0 {
				rf.nextIndex[server] = args.Entries[len(args.Entries)-1].LogIndex - 1
				rf.matchIndex[server] = rf.nextIndex[server] - 1
			}
		} else {
			rf.nextIndex[server] = reply.NextIndex
		}
	}
	return ok
}

func (rf *Raft) boatcastAppendEntries() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	N := rf.commitIndex
	last := rf.getLastIndex()
	baseIndex := rf.log[0].LogIndex
	for i := rf.commitIndex; i <= last; i++ {
		num := 1
		for j := range rf.peers {
			if j != rf.me && rf.matchIndex[j] >= i && rf.log[i-baseIndex].LogTerm == rf.currentTerm {
				num++
			}
		}
		if 2*num > len(rf.peers) {
			N = i
		}
	}
	if N != rf.commitIndex {
		rf.commitIndex = N
		rf.chanCommit <- true
	}

	for i := range rf.peers {
		if i != rf.me && rf.state == STATE_LEADER {

			//copy(args.Entries, rf.log[args.PrevLogIndex + 1 :])

			if rf.nextIndex[i] > baseIndex {
				var args AppendEntriesArgs
				args.Term = rf.currentTerm
				args.LeaderId = rf.me
				args.PrevLogIndex = rf.nextIndex[i] - 1
				// fmt.Printf("baseIndex: %d PrevLogIndex: %d\n", baseIndex, args.PrevLogIndex)
				args.PrevLogTerm = rf.log[args.PrevLogIndex+1-baseIndex].LogTerm
				//args.Entries = make([]LogEntry, len(rf.log[args.PrevLogIndex+1:]))
				args.Entries = make([]LogEntry, len(rf.log[args.PrevLogIndex+1-baseIndex:]))
				copy(args.Entries, rf.log[args.PrevLogIndex+1-baseIndex:])
				args.LeaderCommit = rf.commitIndex
				go func(i int, args AppendEntriesArgs) {
					var reply AppendEntriesReply
					rf.sendAppendEntries(i, args, &reply)
				}(i, args)
			} else {
				var args InstallSnapshotArgs
				args.Term = rf.currentTerm
				args.LeaderId = rf.me
				args.LastIncludeIndex = rf.log[0].LogIndex
				args.LastIncludeTerm = rf.log[0].LogTerm
				args.Data = rf.persister.snapshot
				go func(server int, args InstallSnapshotArgs) {
					reply := &InstallSnapshotArgs{}
					rf.sendInstallSnapshot(server, args, reply)
				}(i, args)
			}
		}
	}

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

	go func() {
		for {
			switch rf.state {
			case STATE_FOLLOWER:
				select {
				case <-rf.chanHeartbeat:
				case <-rf.chanGrantVote:
				case <-time.After(time.Duration(rand.Int63()%333+550) * time.Millisecond):
					rf.state = STATE_CANDIDATE
				}
			case STATE_LEADER:
				//fmt.Printf("Leader:%v %v\n",rf.me,"boatcastAppendEntries	")
				rf.boatcastAppendEntries()
				time.Sleep(HBINTERVAL)
			case STATE_CANDIDATE:
				rf.mu.Lock()
				rf.currentTerm++
				rf.votedFor = rf.me
				rf.voteCount = 1
				rf.persist()
				rf.mu.Unlock()
				go rf.boatcastAppendEntries()
				select {
				case <-time.After(time.Duration(rand.Int63()%333+550) * time.Millisecond):
				case <-rf.chanHeartbeat:
					rf.state = STATE_FOLLOWER
				case <-rf.chanLeader:
					rf.mu.Lock()
					rf.state = STATE_LEADER
					rf.nextIndex = make([]int, len(rf.peers))
					rf.matchIndex = make([]int, len(rf.peers))
					for i := range rf.peers {
						rf.nextIndex[i] = rf.getLastIndex() + 1
						rf.matchIndex[i] = 0
					}
					rf.mu.Unlock()
				}
			}

		}
	}()

	go func() {
		for {
			select {
			case <-rf.chanCommit:
				rf.mu.Lock()
				commitIndex := rf.commitIndex
				baseIndex := rf.log[0].LogIndex
				for i := rf.lastApplied + 1; i <= commitIndex; i++ {
					msg := ApplyMsg{Index: i, Command: rf.log[i-baseIndex].LogComd}
					applyCh <- msg
					rf.lastApplied = i
				}
				rf.mu.Unlock()
			}
		}
	}()

	return rf
}
