package compose

import (
	"reflect"
	"strings"
	"testing"

	"github.com/compose-spec/compose-go/v2/types"
)

// TestServiceFieldsClassified fails when compose-go has a service key that
// is neither supported nor listed as unsupported, so a compose-go upgrade
// cannot slip a new key past validation unnoticed.
func TestServiceFieldsClassified(t *testing.T) {
	seen := map[string]bool{}
	for _, f := range fields(reflect.ValueOf(types.ServiceConfig{})) {
		seen[f.Key] = true
		sup, uns := serviceSupported[f.Key], serviceUnsupported[f.Key]
		switch {
		case sup && uns:
			t.Errorf("key %q is in both lists", f.Key)
		case !sup && !uns:
			t.Errorf("compose-go key %q is not classified: add it to serviceSupported (and translate it) or serviceUnsupported", f.Key)
		}
	}
	for k := range serviceSupported {
		if !seen[k] {
			t.Errorf("supported key %q no longer exists in compose-go", k)
		}
	}
	for k := range serviceUnsupported {
		if !seen[k] {
			t.Errorf("unsupported key %q no longer exists in compose-go", k)
		}
	}
}

// hasIssue reports whether issues contain one for service whose key has
// the given prefix.
func hasIssue(issues []Issue, service, key string) bool {
	for _, i := range issues {
		if i.Service == service && strings.HasPrefix(i.Key, key) {
			return true
		}
	}
	return false
}

func TestValidateSupportedProjectHasNoErrors(t *testing.T) {
	p, meta := loadInline(t, `
services:
  db:
    image: alpine:3.20
    command: sleep infinity
    healthcheck:
      test: ["CMD", "test", "-f", "/tmp/ok"]
      interval: 2s
      timeout: 1s
      start_period: 1s
      retries: 3
    volumes:
      - data:/data
      - ./conf:/conf:ro
      - type: tmpfs
        target: /cache
    networks:
      back:
        aliases: [database]
        ipv4_address: 10.77.0.10
    restart: unless-stopped
    stop_grace_period: 1500ms
    stop_signal: SIGTERM
  app:
    image: alpine:3.20
    entrypoint: ["/bin/sh", "-c"]
    command: ["sleep 1000"]
    environment:
      A: "1"
    labels:
      x.y: z
    ports:
      - "127.0.0.1:8080:80/tcp"
      - "53/udp"
    depends_on:
      db:
        condition: service_healthy
    networks: [back, front]
    hostname: app
    domainname: example.test
    user: "1000:1000"
    working_dir: /work
    shm_size: 64m
    ulimits:
      nofile:
        soft: 1024
        hard: 2048
      nproc: 512
    dns: [1.1.1.1]
    dns_search: [example.test]
    dns_opt: [ndots:2]
    tmpfs: [/run]
    tty: true
    stdin_open: true
    pull_policy: missing
    deploy:
      replicas: 2
      resources:
        limits: {memory: 512m}
  gpu:
    image: alpine:3.20
    gpus: all
    mem_limit: 256m
    cpus: 0.5
networks:
  back:
    ipam:
      config:
        - subnet: 10.77.0.0/24
  front:
    internal: true
volumes:
  data:
    labels: {keep: "yes"}
`)
	_, r := Translate(p, meta)
	if !r.OK() {
		t.Fatalf("unexpected errors: %v", r.Errors)
	}
}

func TestValidateReportsUnsupported(t *testing.T) {
	cases := []struct {
		name, yaml, service, key string
	}{
		{"privileged", "privileged: true", "s", "privileged"},
		{"cap_add", "cap_add: [NET_ADMIN]", "s", "cap_add"},
		{"cap_drop", "cap_drop: [ALL]", "s", "cap_drop"},
		{"devices", "devices: [/dev/fuse]", "s", "devices"},
		{"network_mode host", "network_mode: host", "s", "network_mode"},
		{"pid", "pid: host", "s", "pid"},
		{"ipc", "ipc: host", "s", "ipc"},
		{"sysctls", "sysctls: {net.core.somaxconn: 1024}", "s", "sysctls"},
		{"extra_hosts", "extra_hosts: [\"a:1.2.3.4\"]", "s", "extra_hosts"},
		{"links", "links: [other]\n  other:\n    image: alpine:3.20", "s", "links"},
		{"external_links", "external_links: [x]", "s", "external_links"},
		{"security_opt", "security_opt: [no-new-privileges]", "s", "security_opt"},
		{"userns_mode", "userns_mode: host", "s", "userns_mode"},
		{"cgroup", "cgroup: host", "s", "cgroup"},
		{"platform", "platform: linux/amd64", "s", "platform"},
		{"init", "init: true", "s", "init"},
		{"logging", "logging: {driver: json-file}", "s", "logging"},
		{"mac_address", "mac_address: 02:42:ac:11:00:02", "s", "mac_address"},
		{"read_only", "read_only: true", "s", "read_only"},
		{"secrets", "secrets: [token]", "s", "secrets"},
		{"anonymous volume", "volumes: [/data]", "s", "volumes[0]"},
		{"tmpfs size", "volumes: [{type: tmpfs, target: /t, tmpfs: {size: 1000}}]", "s", "volumes[0].tmpfs.size"},
		{"bind propagation", "volumes: [{type: bind, source: ., target: /t, bind: {propagation: rshared}}]", "s", "volumes[0].bind.propagation"},
		{"entrypoint cleared", "entrypoint: []", "s", "entrypoint"},
		{"healthcheck start_interval", "healthcheck: {test: [CMD, \"true\"], start_interval: 1s}", "s", "healthcheck.start_interval"},
		{"gpu count", "deploy: {resources: {reservations: {devices: [{capabilities: [gpu], count: 1}]}}}", "s", "deploy.resources.reservations.devices[0].count"},
		{"deploy placement", "deploy: {placement: {constraints: [node.role==manager]}}", "s", "deploy.placement"},
		{"reservations memory", "deploy: {resources: {reservations: {memory: 1g}}}", "s", "deploy.resources.reservations.memory"},
		{"sctp", "ports: [\"80/sctp\"]", "s", "ports[0].protocol"},
		{"pull_policy refresh", "pull_policy: daily", "s", "pull_policy"},
		{"reserved label", "labels: {com.docker.compose.project: x}", "s", "labels.com.docker.compose.project"},
		{"bad restart", "restart: sometimes", "s", "restart"},
		{"build secrets", "build: {context: ., secrets: [x]}", "s", "build.secrets"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			yaml := "services:\n  s:\n    image: alpine:3.20\n    " + c.yaml + "\n"
			if strings.Contains(c.yaml, "secrets: [") {
				yaml += "secrets:\n  token:\n    file: ./token\n  x:\n    file: ./x\n"
			}
			p, meta := loadInline(t, yaml)
			_, r := Translate(p, meta)
			if !hasIssue(r.Errors, c.service, c.key) {
				t.Errorf("no error for %s.%s; errors %v", c.service, c.key, r.Errors)
			}
		})
	}
}

func TestValidateTopLevel(t *testing.T) {
	p, meta := loadInline(t, `
services:
  s:
    image: alpine:3.20
    networks: [n1, n2]
    volumes: [v1:/a, v2:/b]
    expose: ["80"]
networks:
  n1:
    driver: overlay
  n2:
    driver_opts: {com.docker.network.bridge.name: br0}
volumes:
  v1:
    driver: nfs
  v2:
    driver_opts: {type: nfs}
`)
	_, r := Translate(p, meta)
	for _, key := range []string{"networks.n1.driver", "networks.n2.driver_opts", "volumes.v1.driver", "volumes.v2.driver_opts"} {
		if !hasIssue(r.Errors, "", key) {
			t.Errorf("no error for %s; errors %v", key, r.Errors)
		}
	}
	if !hasIssue(r.Warnings, "s", "expose") {
		t.Errorf("expose not warned: %v", r.Warnings)
	}
}

func TestValidateVHDVolume(t *testing.T) {
	p, meta := loadInline(t, `
services:
  s:
    image: alpine:3.20
    volumes: [vol:/v]
volumes:
  vol:
    driver: vhd
    driver_opts: {SizeBytes: "1073741824"}
`)
	plan, r := Translate(p, meta)
	if !r.OK() {
		t.Fatal(r.Errors)
	}
	if v := plan.Volumes[0]; v.Driver != "vhd" || v.Opts["SizeBytes"] != "1073741824" {
		t.Errorf("volume plan = %+v", v)
	}
}

func TestValidateHealthyNeedsHealthcheck(t *testing.T) {
	p, meta := loadInline(t, `
services:
  a:
    image: alpine:3.20
  b:
    image: alpine:3.20
    depends_on:
      a: {condition: service_healthy}
`)
	_, r := Translate(p, meta)
	if !hasIssue(r.Errors, "b", "depends_on.a") {
		t.Errorf("errors = %v", r.Errors)
	}
}
