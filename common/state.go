package common

type EtcdConfig struct {
	Name       string `json:"name"`
	Task       string `json:"task"`
	Host       string `json:"host"`
	RpcPort    uint64 `json:"rpcPort"`
	ClientPort uint64 `json:"clientPort"`
	Type       string `json:"type"`
	SlaveID    string `json:"slaveID"`
}
