package server

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const flowInspectorCommandTimeout = 1800 * time.Millisecond

var (
	conntrackTuplePattern = regexp.MustCompile(`src=(\S+)\s+dst=(\S+)\s+sport=(\d+)\s+dport=(\d+)(?:\s+packets=(\d+)\s+bytes=(\d+))?`)
	conntrackMarkPattern  = regexp.MustCompile(`\bmark=([0-9xa-fA-F]+)\b`)
)

type conntrackFlowSample struct {
	Key             string
	Protocol        string
	SourceIP        string
	SourcePort      int
	DestinationIP   string
	DestinationPort int
	UploadBytes     uint64
	DownloadBytes   uint64
	Mark            uint32
}

type conntrackRunner interface {
	Snapshot(ctx context.Context) ([]conntrackFlowSample, error)
}

type conntrackCLIRunner struct{}

func (conntrackCLIRunner) Snapshot(ctx context.Context) ([]conntrackFlowSample, error) {
	runCtx, cancel := context.WithTimeout(ctx, flowInspectorCommandTimeout)
	defer cancel()
	output, err := exec.CommandContext(runCtx, "conntrack", "-L", "-o", "extended").Output()
	if err != nil {
		return nil, fmt.Errorf("conntrack snapshot failed: %w", err)
	}
	return parseConntrackSnapshot(string(output)), nil
}

func parseConntrackSnapshot(raw string) []conntrackFlowSample {
	lines := strings.Split(raw, "\n")
	flows := make([]conntrackFlowSample, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		sample, ok := parseConntrackLine(line)
		if !ok {
			continue
		}
		if _, exists := seen[sample.Key]; exists {
			continue
		}
		seen[sample.Key] = struct{}{}
		flows = append(flows, sample)
	}
	return flows
}

func parseConntrackLine(rawLine string) (conntrackFlowSample, bool) {
	line := strings.TrimSpace(rawLine)
	if line == "" {
		return conntrackFlowSample{}, false
	}
	protocol, protocolOK := detectConntrackProtocol(line)
	if !protocolOK {
		return conntrackFlowSample{}, false
	}
	tuples := conntrackTuplePattern.FindAllStringSubmatch(line, -1)
	if len(tuples) < 1 {
		return conntrackFlowSample{}, false
	}
	sourcePort, sourcePortOK := parseIntStrict(tuples[0][3])
	destinationPort, destinationPortOK := parseIntStrict(tuples[0][4])
	if !sourcePortOK || !destinationPortOK || sourcePort <= 0 || destinationPort <= 0 {
		return conntrackFlowSample{}, false
	}
	uploadBytes, _ := parseUintStrict(tuples[0][6])
	downloadBytes := uint64(0)
	if len(tuples) > 1 {
		downloadBytes, _ = parseUintStrict(tuples[1][6])
	}

	mark := uint32(0)
	markMatch := conntrackMarkPattern.FindStringSubmatch(line)
	if len(markMatch) == 2 {
		parsedMark, ok := parseConntrackMark(markMatch[1])
		if ok {
			mark = parsedMark
		}
	}

	key := flowSampleKey(
		protocol,
		tuples[0][1],
		sourcePort,
		tuples[0][2],
		destinationPort,
	)
	return conntrackFlowSample{
		Key:             key,
		Protocol:        protocol,
		SourceIP:        tuples[0][1],
		SourcePort:      sourcePort,
		DestinationIP:   tuples[0][2],
		DestinationPort: destinationPort,
		UploadBytes:     uploadBytes,
		DownloadBytes:   downloadBytes,
		Mark:            mark,
	}, true
}

func detectConntrackProtocol(line string) (string, bool) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(line)))
	for _, field := range fields {
		switch strings.TrimSpace(field) {
		case "tcp", "udp":
			return field, true
		}
	}
	return "", false
}

func flowSampleKey(protocol string, sourceIP string, sourcePort int, destinationIP string, destinationPort int) string {
	return fmt.Sprintf(
		"%s|%s|%d|%s|%d",
		strings.ToLower(strings.TrimSpace(protocol)),
		strings.TrimSpace(sourceIP),
		sourcePort,
		strings.TrimSpace(destinationIP),
		destinationPort,
	)
}

func parseIntStrict(raw string) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseUintStrict(raw string) (uint64, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}
	value, err := strconv.ParseUint(trimmed, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseConntrackMark(raw string) (uint32, bool) {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(trimmed, "0x") {
		base = 16
		trimmed = strings.TrimPrefix(trimmed, "0x")
	}
	value, err := strconv.ParseUint(trimmed, base, 32)
	if err != nil {
		return 0, false
	}
	return uint32(value), true
}
