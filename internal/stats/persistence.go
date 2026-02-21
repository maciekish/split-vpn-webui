package stats

import (
	"database/sql"
	"fmt"
	"time"
)

type historyRow struct {
	name      string
	timestamp time.Time
	rxBytes   uint64
	txBytes   uint64
}

// Persist writes in-memory interface history to the stats_history table.
func (c *Collector) Persist(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is required")
	}

	c.mu.RLock()
	rows := make([]historyRow, 0, len(c.interfaces)*c.historyLength)
	for name, iface := range c.interfaces {
		if iface == nil || len(iface.History) == 0 {
			continue
		}
		for _, point := range iface.History {
			rows = append(rows, historyRow{
				name:      name,
				timestamp: point.Timestamp,
				rxBytes:   point.RxBytes,
				txBytes:   point.TxBytes,
			})
		}
	}
	c.mu.RUnlock()

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM stats_history`); err != nil {
		return err
	}
	if len(rows) == 0 {
		return tx.Commit()
	}

	stmt, err := tx.Prepare(`
		INSERT INTO stats_history (interface, timestamp, rx_bytes, tx_bytes)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, row := range rows {
		if _, err := stmt.Exec(row.name, row.timestamp.Unix(), row.rxBytes, row.txBytes); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LoadHistory restores history from the stats_history table.
func (c *Collector) LoadHistory(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is required")
	}

	rows, err := db.Query(`
		SELECT interface, timestamp, rx_bytes, tx_bytes
		FROM stats_history
		ORDER BY interface ASC, timestamp ASC
	`)
	if err != nil {
		return err
	}
	defer rows.Close()

	loaded := make(map[string][]historyRow)
	for rows.Next() {
		var (
			name      string
			timestamp int64
			rxBytes   uint64
			txBytes   uint64
		)
		if err := rows.Scan(&name, &timestamp, &rxBytes, &txBytes); err != nil {
			return err
		}
		loaded[name] = append(loaded[name], historyRow{
			name:      name,
			timestamp: time.Unix(timestamp, 0),
			rxBytes:   rxBytes,
			txBytes:   txBytes,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for name, history := range loaded {
		if len(history) > c.historyLength {
			history = history[len(history)-c.historyLength:]
		}
		if iface, ok := c.interfaces[name]; ok {
			c.applyHistoryLocked(iface, history)
			continue
		}
		c.pendingHistory[name] = append([]historyRow(nil), history...)
	}
	return nil
}

func (c *Collector) applyHistoryLocked(iface *InterfaceStats, history []historyRow) {
	if iface == nil {
		return
	}
	if len(history) == 0 {
		iface.History = iface.History[:0]
		iface.Available = false
		iface.LastUpdated = time.Time{}
		iface.RxBytes = 0
		iface.TxBytes = 0
		iface.TotalBytes = 0
		iface.CurrentThroughput = 0
		iface.CurrentRxThroughput = 0
		iface.CurrentTxThroughput = 0
		return
	}

	iface.History = iface.History[:0]
	var previous *historyRow
	for i := range history {
		row := history[i]
		rxThroughput := float64(0)
		txThroughput := float64(0)
		if previous != nil {
			seconds := row.timestamp.Sub(previous.timestamp).Seconds()
			if seconds > 0 {
				if row.rxBytes >= previous.rxBytes {
					rxThroughput = (float64(row.rxBytes-previous.rxBytes) / seconds) * 8
				}
				if row.txBytes >= previous.txBytes {
					txThroughput = (float64(row.txBytes-previous.txBytes) / seconds) * 8
				}
			}
		}
		iface.History = append(iface.History, datapoint{
			Timestamp:       row.timestamp,
			RxThroughput:    rxThroughput,
			TxThroughput:    txThroughput,
			TotalThroughput: rxThroughput + txThroughput,
			RxBytes:         row.rxBytes,
			TxBytes:         row.txBytes,
		})
		previous = &row
	}
	if len(iface.History) > c.historyLength {
		iface.History = iface.History[len(iface.History)-c.historyLength:]
	}

	last := history[len(history)-1]
	iface.Available = true
	iface.LastUpdated = last.timestamp
	iface.RxBytes = last.rxBytes
	iface.TxBytes = last.txBytes
	iface.TotalBytes = last.rxBytes + last.txBytes
	iface.baseRx = 0
	iface.baseTx = 0

	lastPoint := iface.History[len(iface.History)-1]
	iface.CurrentRxThroughput = lastPoint.RxThroughput
	iface.CurrentTxThroughput = lastPoint.TxThroughput
	iface.CurrentThroughput = lastPoint.TotalThroughput
}
