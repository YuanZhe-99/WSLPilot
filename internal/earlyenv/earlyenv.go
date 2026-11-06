// Package earlyenv sets environment variables MyGo reads while its own
// packages initialize, before main runs. cmd/wslpilot imports it first; Go
// initializes packages in import-path order among those whose imports are
// ready, and this one (github.com/YuanZhe-99/…, needing only os) comes
// before github.com/egoist/mygo/… (doc/en-us/upstream.md, "MyGo").
package earlyenv

import "os"

// FullFrames reports whether whole frames were chosen over MyGo's partial
// redraw (MYGO_PARTIAL=0).
var FullFrames = setFullFrames()

// setFullFrames turns MyGo 0.8's partial redraw off unless the environment
// already decides. With it on, a window whose swap chain lost its contents
// (seen after the window sat in the tray) kept drawing only the regions
// that changed, over an empty frame.
// Returns: whether MYGO_PARTIAL is "0" afterwards.
func setFullFrames() bool {
	if _, set := os.LookupEnv("MYGO_PARTIAL"); !set {
		os.Setenv("MYGO_PARTIAL", "0")
	}
	return os.Getenv("MYGO_PARTIAL") == "0"
}
