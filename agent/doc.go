// Package agent is the agent, implemented on containerd.
//
// [Client] implements the Agent interface of spec/agent. It stores images in
// containerd, creates containers from them, and removes what an earlier run
// left behind. [Container] implements the Container interface of spec/agent
// and runs and commits one container.
package agent
