package epay

import (
	"context"

	"gorm.io/gorm"
)

// pendingInsert is one mapped row ready to be written. key is only used for
// diagnostics and idempotency bookkeeping in the caller.
type pendingInsert struct {
	key  string
	args []any
}

// writeInserts writes the rows in transactions of batchSize. When a batch fails
// it is rolled back and retried row by row so one bad row cannot hide the rest:
// the report then shows the real number of failed rows and their reasons.
func (m *Migrator) writeInserts(ctx context.Context, target *targetStore, statement string, rows []pendingInsert, counts *counters) error {
	if len(rows) == 0 {
		return nil
	}
	if target == nil || !target.writable {
		// Dry run: the rows are what would have been written.
		counts.inserted += int64(len(rows))
		return nil
	}
	batchSize := m.options.batchSize()
	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[start:end]
		err := target.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, row := range chunk {
				if execErr := tx.Exec(statement, row.args...).Error; execErr != nil {
					return execErr
				}
			}
			return nil
		})
		if err == nil {
			counts.inserted += int64(len(chunk))
			continue
		}
		for _, row := range chunk {
			if execErr := target.db.WithContext(ctx).Exec(statement, row.args...).Error; execErr != nil {
				counts.failKey(row.key, reasonForError(execErr), execErr.Error())
				continue
			}
			counts.inserted++
		}
	}
	return nil
}

// finishTable assembles the report entry from a counter set.
func finishTable(name, sourceTable, targetTable string, source int64, counts *counters, sourceAmount, targetAmount int64, comparable bool) TableReport {
	report := TableReport{
		Name:              name,
		SourceTable:       sourceTable,
		TargetTable:       targetTable,
		Source:            source,
		Inserted:          counts.inserted,
		Skipped:           counts.skipped,
		Failed:            counts.failed,
		SkipReasons:       counts.skipReasons,
		FailureReasons:    counts.failReasons,
		FailureSamples:    counts.samples,
		SourceAmountCents: sourceAmount,
		TargetAmountCents: targetAmount,
		AmountComparable:  comparable,
	}
	if comparable {
		report.AmountDiffCents = targetAmount - sourceAmount
	}
	if len(report.SkipReasons) == 0 {
		report.SkipReasons = nil
	}
	if len(report.FailureReasons) == 0 {
		report.FailureReasons = nil
	}
	return report
}

// skippedTable builds the report entry for a table whose source is absent.
func skippedTable(name, sourceTable, targetTable, note string) TableReport {
	return TableReport{Name: name, SourceTable: sourceTable, TargetTable: targetTable, SkipReasons: nil, Note: note}
}
