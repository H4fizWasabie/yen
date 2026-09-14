package conversation

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegistryResolvesAndExplicitlyLinksAdapterIdentities(t *testing.T) {
	registry, err := OpenRegistry(filepath.Join(t.TempDir(), "links.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := registry.Resolve("telegram", "chat:42", "work-a")
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := registry.Resolve("telegram", "chat:42", "work-a")
	if err != nil || repeated.ConversationID != first.ConversationID {
		t.Fatalf("repeated = %#v err=%v", repeated, err)
	}
	other, err := registry.Resolve("dashboard", "tab:1", "work-a")
	if err != nil || other.ConversationID == first.ConversationID {
		t.Fatalf("other = %#v err=%v", other, err)
	}
	linked, err := registry.Link("dashboard", "tab:1", first.ConversationID, "work-a")
	if err != nil || linked.ConversationID != first.ConversationID {
		t.Fatalf("linked = %#v err=%v", linked, err)
	}
}

func TestRegistryReloadsAcrossProcessesBeforeResolving(t *testing.T) {
	path := filepath.Join(t.TempDir(), "links.jsonl")
	first, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	one, err := first.Resolve("cli", "cwd", "work")
	if err != nil {
		t.Fatal(err)
	}
	two, err := second.Resolve("cli", "cwd", "work")
	if err != nil {
		t.Fatal(err)
	}
	if one.ConversationID != two.ConversationID {
		t.Fatalf("cross-process identities differ: %#v %#v", one, two)
	}
}

func TestQueueIsFIFOAndRequeuesOrphanedActiveTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	queue, err := OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	first, err := queue.Enqueue("conv-1", "cli", "cwd", "work", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := queue.Enqueue("conv-1", "telegram", "chat:1", "work", "second")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := queue.Claim("conv-2"); err != nil || ok {
		t.Fatalf("other conversation claim = %v, %v", ok, err)
	}
	claimed, ok, err := queue.ClaimFor("conv-1", "worker-a", -time.Second)
	if err != nil || !ok || claimed.ID != first.ID {
		t.Fatalf("first claim = %#v, %v, %v", claimed, ok, err)
	}
	reopened, err := OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	reclaimed, ok, err := reopened.ClaimFor("conv-1", "worker-b", time.Minute)
	if err != nil || !ok || reclaimed.ID != first.ID {
		t.Fatalf("reclaimed = %#v, %v, %v", reclaimed, ok, err)
	}
	if err := reopened.CompleteFor(reclaimed.ID, "worker-b"); err != nil {
		t.Fatal(err)
	}
	next, ok, err := reopened.Claim("conv-1")
	if err != nil || !ok || next.ID != second.ID {
		t.Fatalf("next = %#v, %v, %v", next, ok, err)
	}
}

func TestQueueFileLockAllowsOnlyOneOwnerToClaim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	first, err := OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Enqueue("conv-1", "cli", "cwd", "work", "prompt"); err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := first.Claim("conv-1")
	if err != nil || !ok {
		t.Fatalf("first claim = %#v, %v, %v", claimed, ok, err)
	}
	if _, ok, err := second.Claim("conv-1"); err != nil || ok {
		t.Fatalf("second claim = %v, %v", ok, err)
	}
	if err := first.Complete(claimed.ID); err != nil {
		t.Fatal(err)
	}
}

func TestQueueCancelActiveUsesDurableLeaseOwnerAcrossHandles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	first, err := OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := first.Enqueue("conv-1", "dashboard", "tab-1", "work", "prompt")
	if err != nil {
		t.Fatal(err)
	}
	claimed, ok, err := first.Claim("conv-1")
	if err != nil || !ok || claimed.ID != turn.ID {
		t.Fatalf("claim=%#v ok=%v err=%v", claimed, ok, err)
	}
	if err := second.CancelActive(turn.ID); err != nil {
		t.Fatal(err)
	}
	if active := first.Active("conv-1"); len(active) != 0 {
		t.Fatalf("active after cross-handle cancel=%#v", active)
	}
}

func TestQueueFileLockAllowsOnlyOneSubprocessToClaim(t *testing.T) {
	if os.Getenv("YEN_QUEUE_HELPER") == "1" {
		return
	}
	path := filepath.Join(t.TempDir(), "queue.jsonl")
	queue, err := OpenQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := queue.Enqueue("conv-process", "cli", "cwd", "work", "prompt"); err != nil {
		t.Fatal(err)
	}
	results := make([]string, 2)
	commands := make([]*exec.Cmd, 2)
	for i := range commands {
		resultPath := filepath.Join(t.TempDir(), "result")
		commands[i] = exec.Command(os.Args[0], "-test.run=TestQueueProcessHelper$")
		commands[i].Env = append(os.Environ(), "YEN_QUEUE_HELPER=1", "YEN_QUEUE_PATH="+path, "YEN_QUEUE_RESULT="+resultPath)
		commands[i].Stdout = os.Stdout
		commands[i].Stderr = os.Stderr
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
		defer func(command *exec.Cmd) { _ = command.Process.Kill() }(commands[i])
		results[i] = resultPath
	}
	for _, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	claimed := 0
	for _, resultPath := range results {
		data, err := os.ReadFile(resultPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(data)) != "" {
			claimed++
		}
	}
	if claimed != 1 {
		t.Fatalf("subprocess claims=%d, want exactly one", claimed)
	}
}

func TestQueueProcessHelper(t *testing.T) {
	if os.Getenv("YEN_QUEUE_HELPER") != "1" {
		return
	}
	queue, err := OpenQueue(os.Getenv("YEN_QUEUE_PATH"))
	if err != nil {
		t.Fatal(err)
	}
	turn, ok, err := queue.Claim("conv-process")
	if err != nil {
		t.Fatal(err)
	}
	value := ""
	if ok {
		value = turn.ID
		time.Sleep(300 * time.Millisecond)
	}
	if err := os.WriteFile(os.Getenv("YEN_QUEUE_RESULT"), []byte(value), 0o600); err != nil {
		t.Fatal(err)
	}
}
