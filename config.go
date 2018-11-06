package raft

import (
	crand "crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"runtime"
	"sync"
	"testing"

	"project_01/rpc-realize"
)

func randstring(n int) string {
	b := make([]byte, 2*n)
	crand.Read(b)                             // 读取指定长度的字节 是获取随机数的最好方法
	s := base64.URLEncoding.EncodeToString(b) // URLEncoding ROF 4648定义的另一种base64编码字符集,用于URL和文件名
	return s[:n]
}

type config struct {
	mu        sync.Mutex
	t         *testing.T
	net       *labrpc.Network
	n         int           // server个数
	done      int32         // 通知内部的线程已经无效
	rafts     []*Raft       // raft节点
	applyErr  []string      // 从回复通道中读取错误信息
	connected []bool        // 是否每个server都在网络中
	saved     []*Persister  // 快照
	endnames  [][]string    // 每个发送到port端口的文件名
	logs      []map[int]int // 保存每一个服务器提交的日志条目
}

func make_config(t *testing.T, n int, unreliable bool) *config {
	runtime.GOMAXPROCS(4)
	// 基本配置
	cfg := &config{}
	cfg.t = t
	cfg.net = labrpc.MakeNetWork()
	cfg.n = n
	cfg.applyErr = make([]string, cfg.n)
	cfg.rafts = make([]*Raft, cfg.n)
	cfg.connected = make([]bool, cfg.n)
	cfg.saved = make([]*Persister, cfg.n)
	cfg.endnames = make([][]string, cfg.n)
	cfg.logs = make([]map[int]int, cfg.n)

	cfg.setunreliable(unreliable)

	cfg.net.LongDelays(true) //连接不可达时， 让服务器休息一段时间

	// 创建一个完全的raft
	for i := 0; i < cfg.n; i++ {
		cfg.logs[i] = map[int]int{}
		cfg.start1(i)
	}
}

func (cfg *config) crash1(i int) {
	cfg.disconnect(i)
	cfg.net.DeleteServer(i) // disable client connections to  the server

	cfg.mu.Lock()
	defer cfg.mu.Unlock()

	// 一个新的persister 防止旧实例继续更新persister，且 复制旧的persister内容
	//以便我们传递Make() 最后一个持久化状态
	if cfg.saved[i] != nil {
		cfg.saved[i] = cfg.saved[i].Copy()
	}

	rf := cfg.rafts[i]
	if rf != nil {
		cfg.mu.Unlock()
		rf.Kill()
		cfg.mu.Lock()
		cfg.rafts[i] = nil
	}

	if cfg.saved[i] != nil {
		raftlog := cfg.saved[i].ReadRaftState()
		cfg.saved[i] = &Persister{}
		cfg.saved[i].SaveRaftState(raftlog)
	}
}

// 开启/重启 raft
// 如果已经存在，则 先'kill',
// 分配新的端口和新的状态 来隔离之前的服务器的实例(因为无法真正的杀死他)
func (cfg *config) start1(i int) {
	cfg.crash1(i)

	cfg.endnames[i] = make([]string, cfg.n)
	for j := 0; j < cfg.n; j++ {
		cfg.endnames[i][j] = randstring(20)
	}

	// 新的客户端集合
	ends := make([]*labrpc.ClientEnd, cfg.n)
	for j := 0; j < cfg.n; j++ {
		ends[j] = cfg.net.MakeEnd(cfg.endnames[i][j])
		cfg.net.Connect(cfg.endnames[i][j], j)
	}

	cfg.mu.Lock()
	if cfg.saved[i] != nil {
		cfg.saved[i] = cfg.saved[i].Copy()
	} else {
		cfg.saved[i] = MakePersister()
	}
	cfg.mu.Unlock()

	// 收听来自raft的消息， 指示新提交的消息
	applyCh := make(chan ApplyMsg)
	go func() {
		for m := range applyCh {
			err_msg := ""
			if m.UseSnapshot {
				// ignore the snapshot
			} else if v, ok := (m.Command).(int); ok {
				cfg.mu.Lock()
				for j := 0; j < len(cfg.logs); j++ {
					if old, oldok := cfg.logs[j][m.Index]; oldok && old != v {
						// some server has already committed a different value for this entry!
						err_msg = fmt.Sprintf("commit index=%v server= %v %v != server= %v %v",
							m.Index, i, m.Command, j, old)
					}
				}
				_, prevok := cfg.logs[i][m.Index-1]
				cfg.logs[i][m.Index] = v
				cfg.mu.Unlock()

				if m.Index > 1 && prevok == false {
					err_msg = fmt.Sprintf("server %v apply out of order %v", i, m.Index)
				}
			} else {
				err_msg = fmt.Sprintf("committed command %v is not an int", m.Command)
			}

			if err_msg != "" {
				log.Fatal("apply error: %v\n", err_msg)
				cfg.applyErr[i] = err_msg
			}
		}
	}()

	rf := Make(ends, i, cfg.saved[i], applyCh)
	cfg.mu.Lock()
	cfg.rafts[i] = rf
	cfg.mu.Unlock()

	svc := labrpc.MakeService(rf)
	srv := labrpc.MakeServer()
	srv.AddService(svc)
	cfg.net.AddServer(i, srv)
}

func (cfg *config) setunreliable(unrel bool) {
	cfg.net.Reliable(!unrel)
}

// 从网络中分离服务器i
func (cfg *config) disconnect(i int) {

	cfg.connected[i] = false

	//outgoing ClientEnds
	for j := 0; j < cfg.n; j++ {
		if cfg.endnames[i] != nil {
			endname := cfg.endnames[i][j]
			cfg.net.Enable(endname, false)
		}
	}

	// incoming ClientEnds
	for j := 0; j < cfg.n; j++ {
		if cfg.endnames[j] != nil {
			endname := cfg.endnames[j][i]
			cfg.net.Enable(endname, false)
		}
	}
}
