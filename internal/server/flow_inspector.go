package server

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	flowInspectorPollIntervalSeconds = 2
	flowInspectorIdleRetention       = 10 * time.Minute
	flowInspectorSessionTTL          = 30 * time.Minute
)

var (
	errFlowInspectorSessionNotFound = errors.New("flow inspector session not found")
	errFlowInspectorVPNMismatch     = errors.New("flow inspector session does not match vpn")
)

type flowInspectorSample struct {
	Key               string
	Protocol          string
	SourceIP          string
	SourcePort        int
	SourceMAC         string
	SourceDeviceName  string
	SourceInterface   string
	DestinationIP     string
	DestinationPort   int
	DestinationDomain string
	UploadBytes       uint64
	DownloadBytes     uint64
}

type flowInspectorSnapshot struct {
	SessionID            string             `json:"sessionId"`
	VPNName              string             `json:"vpnName"`
	InterfaceName        string             `json:"interfaceName"`
	GeneratedAt          time.Time          `json:"generatedAt"`
	IdleRetentionSeconds int                `json:"idleRetentionSeconds"`
	PollIntervalSeconds  int                `json:"pollIntervalSeconds"`
	FlowCount            int                `json:"flowCount"`
	Totals               flowInspectorTotal `json:"totals"`
	Flows                []flowInspectorRow `json:"flows"`
}

type flowInspectorTotal struct {
	UploadBytes   uint64 `json:"uploadBytes"`
	DownloadBytes uint64 `json:"downloadBytes"`
	TotalBytes    uint64 `json:"totalBytes"`
}

type flowInspectorRow struct {
	Key               string    `json:"key"`
	Protocol          string    `json:"protocol"`
	SourceIP          string    `json:"sourceIp"`
	SourcePort        int       `json:"sourcePort"`
	SourceMAC         string    `json:"sourceMac,omitempty"`
	SourceDeviceName  string    `json:"sourceDeviceName,omitempty"`
	SourceInterface   string    `json:"sourceInterface,omitempty"`
	DestinationIP     string    `json:"destinationIp"`
	DestinationPort   int       `json:"destinationPort"`
	DestinationDomain string    `json:"destinationDomain,omitempty"`
	UploadBps         float64   `json:"uploadBps"`
	DownloadBps       float64   `json:"downloadBps"`
	UploadBytes       uint64    `json:"uploadBytes"`
	DownloadBytes     uint64    `json:"downloadBytes"`
	TotalBytes        uint64    `json:"totalBytes"`
	LastSeen          time.Time `json:"lastSeen"`
}

type vpnFlowInspector struct {
	mu       sync.Mutex
	sessions map[string]*vpnFlowSession
	now      func() time.Time
}

type vpnFlowSession struct {
	ID            string
	VPNName       string
	InterfaceName string
	CreatedAt     time.Time
	LastTouched   time.Time
	TotalUpload   uint64
	TotalDownload uint64
	FlowByKey     map[string]*vpnFlowRecord
}

type vpnFlowRecord struct {
	Protocol          string
	SourceIP          string
	SourcePort        int
	SourceMAC         string
	SourceDeviceName  string
	SourceInterface   string
	DestinationIP     string
	DestinationPort   int
	DestinationDomain string
	LastSeen          time.Time
	LastSampleAt      time.Time
	LastUploadBytes   uint64
	LastDownloadBytes uint64
	UploadBps         float64
	DownloadBps       float64
	UploadTotal       uint64
	DownloadTotal     uint64
}

func newVPNFlowInspector() *vpnFlowInspector {
	return &vpnFlowInspector{
		sessions: make(map[string]*vpnFlowSession),
		now:      time.Now,
	}
}

func (i *vpnFlowInspector) startSession(vpnName string, interfaceName string) (string, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now().UTC()
	i.cleanupExpiredSessionsLocked(now)
	sessionID, err := newFlowInspectorSessionID()
	if err != nil {
		return "", err
	}
	i.sessions[sessionID] = &vpnFlowSession{
		ID:            sessionID,
		VPNName:       strings.TrimSpace(vpnName),
		InterfaceName: strings.TrimSpace(interfaceName),
		CreatedAt:     now,
		LastTouched:   now,
		FlowByKey:     make(map[string]*vpnFlowRecord),
	}
	return sessionID, nil
}

func (i *vpnFlowInspector) stopSession(vpnName string, sessionID string) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	session, ok := i.sessions[sessionID]
	if !ok {
		return errFlowInspectorSessionNotFound
	}
	if session.VPNName != strings.TrimSpace(vpnName) {
		return errFlowInspectorVPNMismatch
	}
	delete(i.sessions, sessionID)
	return nil
}

func (i *vpnFlowInspector) updateAndSnapshot(vpnName string, sessionID string, samples []flowInspectorSample) (flowInspectorSnapshot, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	now := i.now().UTC()
	i.cleanupExpiredSessionsLocked(now)
	session, ok := i.sessions[sessionID]
	if !ok {
		return flowInspectorSnapshot{}, errFlowInspectorSessionNotFound
	}
	if session.VPNName != strings.TrimSpace(vpnName) {
		return flowInspectorSnapshot{}, errFlowInspectorVPNMismatch
	}
	session.LastTouched = now

	seen := make(map[string]struct{}, len(samples))
	for _, sample := range samples {
		key := strings.TrimSpace(sample.Key)
		if key == "" {
			continue
		}
		record, exists := session.FlowByKey[key]
		if !exists {
			session.FlowByKey[key] = &vpnFlowRecord{
				Protocol:          sample.Protocol,
				SourceIP:          sample.SourceIP,
				SourcePort:        sample.SourcePort,
				SourceMAC:         sample.SourceMAC,
				SourceDeviceName:  sample.SourceDeviceName,
				SourceInterface:   sample.SourceInterface,
				DestinationIP:     sample.DestinationIP,
				DestinationPort:   sample.DestinationPort,
				DestinationDomain: sample.DestinationDomain,
				LastSeen:          now,
				LastSampleAt:      now,
				LastUploadBytes:   sample.UploadBytes,
				LastDownloadBytes: sample.DownloadBytes,
			}
			seen[key] = struct{}{}
			continue
		}

		elapsed := now.Sub(record.LastSampleAt).Seconds()
		if elapsed <= 0 {
			elapsed = float64(flowInspectorPollIntervalSeconds)
		}
		uploadDelta := monotonicDelta(sample.UploadBytes, record.LastUploadBytes)
		downloadDelta := monotonicDelta(sample.DownloadBytes, record.LastDownloadBytes)
		record.Protocol = sample.Protocol
		record.SourceIP = sample.SourceIP
		record.SourcePort = sample.SourcePort
		record.SourceMAC = sample.SourceMAC
		record.SourceDeviceName = sample.SourceDeviceName
		record.SourceInterface = sample.SourceInterface
		record.DestinationIP = sample.DestinationIP
		record.DestinationPort = sample.DestinationPort
		record.DestinationDomain = sample.DestinationDomain
		record.UploadBps = float64(uploadDelta*8) / elapsed
		record.DownloadBps = float64(downloadDelta*8) / elapsed
		record.UploadTotal += uploadDelta
		record.DownloadTotal += downloadDelta
		record.LastUploadBytes = sample.UploadBytes
		record.LastDownloadBytes = sample.DownloadBytes
		record.LastSampleAt = now
		record.LastSeen = now
		session.TotalUpload += uploadDelta
		session.TotalDownload += downloadDelta
		seen[key] = struct{}{}
	}

	for key, record := range session.FlowByKey {
		if _, active := seen[key]; active {
			continue
		}
		if now.Sub(record.LastSeen) > flowInspectorIdleRetention {
			delete(session.FlowByKey, key)
			continue
		}
		record.UploadBps = 0
		record.DownloadBps = 0
	}

	rows := make([]flowInspectorRow, 0, len(session.FlowByKey))
	for key, record := range session.FlowByKey {
		rows = append(rows, flowInspectorRow{
			Key:               key,
			Protocol:          record.Protocol,
			SourceIP:          record.SourceIP,
			SourcePort:        record.SourcePort,
			SourceMAC:         record.SourceMAC,
			SourceDeviceName:  record.SourceDeviceName,
			SourceInterface:   record.SourceInterface,
			DestinationIP:     record.DestinationIP,
			DestinationPort:   record.DestinationPort,
			DestinationDomain: record.DestinationDomain,
			UploadBps:         record.UploadBps,
			DownloadBps:       record.DownloadBps,
			UploadBytes:       record.UploadTotal,
			DownloadBytes:     record.DownloadTotal,
			TotalBytes:        record.UploadTotal + record.DownloadTotal,
			LastSeen:          record.LastSeen,
		})
	}
	sort.Slice(rows, func(left, right int) bool {
		leftRate := rows[left].UploadBps + rows[left].DownloadBps
		rightRate := rows[right].UploadBps + rows[right].DownloadBps
		if leftRate == rightRate {
			return rows[left].Key < rows[right].Key
		}
		return leftRate > rightRate
	})

	return flowInspectorSnapshot{
		SessionID:            session.ID,
		VPNName:              session.VPNName,
		InterfaceName:        session.InterfaceName,
		GeneratedAt:          now,
		IdleRetentionSeconds: int(flowInspectorIdleRetention.Seconds()),
		PollIntervalSeconds:  flowInspectorPollIntervalSeconds,
		FlowCount:            len(rows),
		Totals: flowInspectorTotal{
			UploadBytes:   session.TotalUpload,
			DownloadBytes: session.TotalDownload,
			TotalBytes:    session.TotalUpload + session.TotalDownload,
		},
		Flows: rows,
	}, nil
}

func (i *vpnFlowInspector) cleanupExpiredSessionsLocked(now time.Time) {
	for id, session := range i.sessions {
		if now.Sub(session.LastTouched) > flowInspectorSessionTTL {
			delete(i.sessions, id)
		}
	}
}

func monotonicDelta(current uint64, previous uint64) uint64 {
	if current >= previous {
		return current - previous
	}
	return 0
}

func newFlowInspectorSessionID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
