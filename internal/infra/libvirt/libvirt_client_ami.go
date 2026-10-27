package libvirt

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"libvirt.org/go/libvirt" // use the same libvirt import path as the rest of your file
)

// ---------------------------------------------------------------------
// Shell helpers (file operations run on the hypervisor host)
// ---------------------------------------------------------------------

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
 
var runSeq atomic.Uint64 // correlates all log lines of one call

// truncate keeps log lines readable and bounded.
func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	return s[:max] + fmt.Sprintf("...(+%d bytes)", len(s)-max)
}

// RunOnHost runs a shell command locally when remoteHostIP is empty, otherwise over SSH.
func (l *LibvirtClient) RunOnHost(remoteHostIP, remoteHostUser, remoteHostKey, command string) (string, error) {
	id := runSeq.Add(1)
	start := time.Now()
	logf := func(format string, args ...any) {
		log.Printf("[RunOnHost #%d] "+format, append([]any{id}, args...)...)
	}

	// The service key from config always wins over the per-host key passed in.
	if l.cfg.PrivateKey != "" {
		remoteHostKey = l.cfg.PrivateKey
	}

	// ---------------- local execution ----------------
	if remoteHostIP == "" {
		logf("mode=local cmd=%q", truncate(command, 300))
		out, err := exec.Command("bash", "-c", command).CombinedOutput()
		if err != nil {
			logf("ERROR local command failed after %s: %v output=%q",
				time.Since(start).Round(time.Millisecond), err, truncate(string(out), 500))
			return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		logf("OK local command finished in %s outputBytes=%d",
			time.Since(start).Round(time.Millisecond), len(out))
		return string(out), nil
	}

	// ---------------- SSH execution ----------------
	if remoteHostUser == "" {
		logf("ERROR no ssh user provided for host %s", remoteHostIP)
		return "", fmt.Errorf("no ssh user for host %s", remoteHostIP)
	}
	if strings.TrimSpace(remoteHostKey) == "" {
		logf("ERROR no ssh private key available for %s@%s (cfg.PrivateKey and host key both empty)", remoteHostUser, remoteHostIP)
		return "", fmt.Errorf("no ssh private key for %s@%s", remoteHostUser, remoteHostIP)
	}

	signer, err := ssh.ParsePrivateKey([]byte(remoteHostKey))
	if err != nil {
		logf("ERROR cannot parse private key (len=%d bytes, firstLine=%q): %v",
			len(remoteHostKey), truncate(strings.SplitN(remoteHostKey, "\n", 2)[0], 40), err)
		return "", fmt.Errorf("parse ssh key: %w", err)
	}

	logf("mode=ssh target=%s@%s keyType=%s keyFingerprint=%s cmd=%q",
		remoteHostUser, remoteHostIP, signer.PublicKey().Type(),
		ssh.FingerprintSHA256(signer.PublicKey()), truncate(command, 300))

	cfg := &ssh.ClientConfig{
		User:            remoteHostUser,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: pin host keys in production
		Timeout:         15 * time.Second,
	}

	addr := net.JoinHostPort(remoteHostIP, "22")
	dialStart := time.Now()
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		hint := ""
		switch {
		case strings.Contains(err.Error(), "unable to authenticate"):
			hint = " HINT: server rejected the key; add this public key to ~/.ssh/authorized_keys of " +
				remoteHostUser + " on " + remoteHostIP + " (fingerprint " +
				ssh.FingerprintSHA256(signer.PublicKey()) + "; check `journalctl -u ssh` on the host)"
		case strings.Contains(err.Error(), "i/o timeout"), strings.Contains(err.Error(), "connection refused"):
			hint = " HINT: host unreachable or sshd not listening on :22"
		}
		logf("ERROR ssh dial %s failed after %s: %v.%s",
			addr, time.Since(dialStart).Round(time.Millisecond), err, hint)
		return "", fmt.Errorf("ssh dial: %w", err)
	}
	defer client.Close()
	logf("ssh connected to %s in %s (server=%s)",
		addr, time.Since(dialStart).Round(time.Millisecond), string(client.ServerVersion()))

	session, err := client.NewSession()
	if err != nil {
		logf("ERROR cannot open ssh session: %v", err)
		return "", fmt.Errorf("ssh session: %w", err)
	}
	defer session.Close()

	var buf bytes.Buffer
	session.Stdout = &buf
	session.Stderr = &buf

	runStart := time.Now()
	if err := session.Run(command); err != nil {
		var exitErr *ssh.ExitError
		if errors.As(err, &exitErr) {
			logf("ERROR remote command exited with status %d after %s output=%q",
				exitErr.ExitStatus(), time.Since(runStart).Round(time.Millisecond), truncate(buf.String(), 500))
		} else {
			logf("ERROR remote command failed after %s: %v output=%q",
				time.Since(runStart).Round(time.Millisecond), err, truncate(buf.String(), 500))
		}
		return "", fmt.Errorf("%w: %s", err, strings.TrimSpace(buf.String()))
	}

	logf("OK remote command finished in %s (total %s) outputBytes=%d",
		time.Since(runStart).Round(time.Millisecond), time.Since(start).Round(time.Millisecond), buf.Len())
	return buf.String(), nil
}

// Only allow file operations inside the images directory.
const imagesRoot = "/var/lib/libvirt/images/"

func validateImagePath(p string) error {
	clean := filepath.Clean(p)
	if !strings.HasPrefix(clean, imagesRoot) || strings.Contains(p, "..") {
		return fmt.Errorf("refusing path outside %s: %q", imagesRoot, p)
	}
	return nil
}

// ---------------------------------------------------------------------
// 1. DeleteFile
// ---------------------------------------------------------------------

func (l *LibvirtClient) DeleteFile(remoteHostIP, remoteHostUser, remoteHostKey, path string) error {
	if err := validateImagePath(path); err != nil {
		return err
	}
	_, err := l.RunOnHost(remoteHostIP, remoteHostUser, remoteHostKey, "rm -f "+shellQuote(path))
	return err
}

// ---------------------------------------------------------------------
// 2. WaitForState  (state: "running", "shut off", "paused")
// ---------------------------------------------------------------------

func (l *LibvirtClient) WaitForState(remoteHostIP, remoteHostUser, remoteHostKey, vmName, state string, timeout time.Duration) error {
	var want libvirt.DomainState
	switch strings.ToLower(state) {
	case "running":
		want = libvirt.DOMAIN_RUNNING
	case "shut off", "shutoff", "stopped":
		want = libvirt.DOMAIN_SHUTOFF
	case "paused":
		want = libvirt.DOMAIN_PAUSED
	default:
		return fmt.Errorf("unsupported state %q", state)
	}

	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", vmName, err)
	}
	defer dom.Free()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		cur, _, err := dom.GetState()
		if err != nil {
			return fmt.Errorf("get state: %w", err)
		}
		if cur == want {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out after %s waiting for %s to reach %q", timeout, vmName, state)
}

// ---------------------------------------------------------------------
// 3. ConvertDisk  (flattens backing chain into a standalone compressed qcow2)
// ---------------------------------------------------------------------
var progressRe = regexp.MustCompile(`\((\d+(?:\.\d+)?)/100%\)`)

// splits on \r or \n, because qemu-img overwrites the same line with \r
func scanCRLF(data []byte, atEOF bool) (int, []byte, error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}


func (l *LibvirtClient) dialSSH(hostIP, user, privateKey string) (*ssh.Client, error) {
	signer, err := ssh.ParsePrivateKey([]byte(strings.TrimSpace(privateKey) + "\n"))
	if err != nil {
		return nil, fmt.Errorf("parse ssh private key: %w", err)
	}

	cfg := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{ssh.PublicKeys(signer)},
		// Matches the no_verify=1 you use for libvirt. See the note below for production.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         15 * time.Second,
	}

	addr := hostIP
	if _, _, err := net.SplitHostPort(hostIP); err != nil {
		addr = net.JoinHostPort(hostIP, "22")
	}

	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s@%s: %w", user, addr, err)
	}

	// Keepalive: the convert runs ~10 minutes, and idle NAT/firewalls can drop the
	// connection if there's no traffic. Stops automatically when the client closes.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
				return
			}
		}
	}()

	return client, nil
}





// onProgress receives 0-100
func (l *LibvirtClient) ConvertDiskWithProgress(
	remoteHostIP, remoteHostUser, remoteHostKey, srcPath, dstPath string,
	onProgress func(pct int),
) error {
	if err := validateImagePath(srcPath); err != nil {
		return err
	}
	if err := validateImagePath(dstPath); err != nil {
		return err
	}

	cmd := fmt.Sprintf("mkdir -p %s && qemu-img convert -U -p -c -O qcow2 %s %s 2>&1",
		shellQuote(filepath.Dir(dstPath)), shellQuote(srcPath), shellQuote(dstPath))

	// Reuse whatever your RunOnHost uses to build the ssh client.
	client, err := l.dialSSH(remoteHostIP, remoteHostUser, remoteHostKey) // *ssh.Client
	if err != nil {
		return err
	}
	defer client.Close()

	sess, err := client.NewSession()
	if err != nil {
		return err
	}
	defer sess.Close()

	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	if err := sess.Start(cmd); err != nil {
		return err
	}

	var tail []string // keep last lines for error context
	sc := bufio.NewScanner(stdout)
	sc.Split(scanCRLF)
	for sc.Scan() {
		line := sc.Text()
		if m := progressRe.FindStringSubmatch(line); m != nil {
			f, _ := strconv.ParseFloat(m[1], 64)
			onProgress(int(f))
			continue
		}
		if line != "" {
			tail = append(tail, line)
			if len(tail) > 5 {
				tail = tail[1:]
			}
		}
	}

	if err := sess.Wait(); err != nil {
		return fmt.Errorf("qemu-img convert failed: %w (output: %v)", err, tail)
	}
	return nil
}



func (l *LibvirtClient) ConvertDisk(remoteHostIP, remoteHostUser, remoteHostKey, srcPath, dstPath string) error {
	if err := validateImagePath(srcPath); err != nil {
		return err
	}
	if err := validateImagePath(dstPath); err != nil {
		return err
	}

	// -U (--force-share) lets us read a disk that a running QEMU still has open.
	// -c compresses; -O qcow2 flattens any template backing file.
	// cmd := fmt.Sprintf("mkdir -p %s",
	// 	shellQuote(filepath.Dir(dstPath)))
			cmd := fmt.Sprintf("mkdir -p %s && qemu-img convert -U -p -c -O qcow2 %s %s",
		shellQuote(filepath.Dir(dstPath)), shellQuote(srcPath), shellQuote(dstPath))

	_, err := l.RunOnHost(remoteHostIP, remoteHostUser, remoteHostKey, cmd)
	return err
}

// ---------------------------------------------------------------------
// 4. CreateDiskOnlySnapshot
//    Returns quiesced=true if the guest agent froze the filesystem.
// ---------------------------------------------------------------------

func (l *LibvirtClient) CreateDiskOnlySnapshot(remoteHostIP, remoteHostUser, remoteHostKey, vmName, diskTarget, overlayPath string, tryQuiesce bool) (bool, error) {
	if err := validateImagePath(overlayPath); err != nil {
		return false, err
	}

	// Make sure the overlay directory exists (libvirt creates the overlay file itself).
	if _, err := l.RunOnHost(remoteHostIP, remoteHostUser, remoteHostKey,
		"mkdir -p "+shellQuote(filepath.Dir(overlayPath))); err != nil {
		return false, fmt.Errorf("prepare overlay dir: %w", err)
	}

	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return false, err
	}
	defer cleanup()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return false, fmt.Errorf("lookup domain %s: %w", vmName, err)
	}
	defer dom.Free()

	snapName := fmt.Sprintf("ami-%d", time.Now().Unix())
	xml := fmt.Sprintf(`<domainsnapshot>
  <name>%s</name>
  <disks>
    <disk name='%s' snapshot='external'>
      <driver type='qcow2'/>
      <source file='%s'/>
    </disk>
  </disks>
</domainsnapshot>`, snapName, diskTarget, overlayPath)

	base := libvirt.DOMAIN_SNAPSHOT_CREATE_DISK_ONLY |
		libvirt.DOMAIN_SNAPSHOT_CREATE_ATOMIC |
		libvirt.DOMAIN_SNAPSHOT_CREATE_NO_METADATA

	if tryQuiesce {
		snap, err := dom.CreateSnapshotXML(xml, base|libvirt.DOMAIN_SNAPSHOT_CREATE_QUIESCE)
		if err == nil {
			if snap != nil {
				snap.Free()
			}
			return true, nil
		}
		// Typically "guest agent is not responding" / "not configured": fall back to crash-consistent.
	}

	snap, err := dom.CreateSnapshotXML(xml, base)
	if err != nil {
		return false, fmt.Errorf("create disk-only snapshot: %w", err)
	}
	if snap != nil {
		snap.Free()
	}
	return false, nil
}

// ---------------------------------------------------------------------
// 5. BlockCommitActive  (merge overlay into base and pivot back to base)
// ---------------------------------------------------------------------

func (l *LibvirtClient) BlockCommitActive(remoteHostIP, remoteHostUser, remoteHostKey, vmName, diskTarget string) error {
	conn, cleanup, err := l.getConnection(remoteHostIP, remoteHostUser, remoteHostKey)
	if err != nil {
		return err
	}
	defer cleanup()

	dom, err := conn.LookupDomainByName(vmName)
	if err != nil {
		return fmt.Errorf("lookup domain %s: %w", vmName, err)
	}
	defer dom.Free()

	// Empty base/top = commit the active layer into its immediate backing file.
	if err := dom.BlockCommit(diskTarget, "", "", 0, libvirt.DOMAIN_BLOCK_COMMIT_ACTIVE); err != nil {
		return fmt.Errorf("start blockcommit: %w", err)
	}

	// Wait until the job has copied everything (cur == end), then pivot.
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		info, err := dom.GetBlockJobInfo(diskTarget, 0)
		if err != nil {
			return fmt.Errorf("get block job info: %w", err)
		}
		if info == nil {
			return fmt.Errorf("blockcommit job for %s disappeared before pivot", diskTarget)
		}
		if info.End > 0 && info.Cur == info.End {
			break
		}
		time.Sleep(2 * time.Second)
	}

	if err := dom.BlockJobAbort(diskTarget, libvirt.DOMAIN_BLOCK_JOB_ABORT_PIVOT); err != nil {
		return fmt.Errorf("pivot blockcommit: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------
// 6. StatAndHash
// ---------------------------------------------------------------------

func (l *LibvirtClient) StatAndHash(remoteHostIP, remoteHostUser, remoteHostKey, path string) (int64, string, error) {
	if err := validateImagePath(path); err != nil {
		return 0, "", err
	}
	q := shellQuote(path)

	sizeOut, err := l.RunOnHost(remoteHostIP, remoteHostUser, remoteHostKey, "stat -c %s "+q)
	if err != nil {
		return 0, "", fmt.Errorf("stat: %w", err)
	}
	size, err := strconv.ParseInt(strings.TrimSpace(sizeOut), 10, 64)
	if err != nil {
		return 0, "", fmt.Errorf("parse size %q: %w", sizeOut, err)
	}

	hashOut, err := l.RunOnHost(remoteHostIP, remoteHostUser, remoteHostKey, "sha256sum "+q)
	if err != nil {
		return 0, "", fmt.Errorf("sha256sum: %w", err)
	}
	fields := strings.Fields(hashOut)
	if len(fields) == 0 {
		return 0, "", fmt.Errorf("unexpected sha256sum output: %q", hashOut)
	}
	return size, fields[0], nil
}