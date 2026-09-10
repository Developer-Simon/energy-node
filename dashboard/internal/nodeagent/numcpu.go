package nodeagent

import "runtime"

func numCPUImpl() int { return runtime.NumCPU() }
