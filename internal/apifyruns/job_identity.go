package apifyruns

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"unicode/utf8"

	"itmap/internal/apifysettings"
)

var jobGenerationPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// HHJobKey строит непрозрачную identity стабильного job и его конкретного шага.
// Lease owner, run и attempt намеренно не входят: после смены владельца тот же
// job/step должен ссылаться на тот же ключ. Передавая ключ в Service.Execute,
// вызывающий дополнительно связывает его с конкретным HH input через существующий
// operationKey и apify_runs; новый денежный журнал не создаётся. Ключ сам по
// себе не подтверждает durable связь с attempt, fence, резерв или право на POST.
// После restore generation меняется: старые paid evidence необходимо проверить
// до разрешения нового запуска. Этот API не подключён к worker runtime.
func HHJobKey(generation, jobID, step string) (string, error) {
	if !jobGenerationPattern.MatchString(generation) || jobID == "" || step == "" || len(jobID) > 256 || len(step) > 512 || !utf8.ValidString(jobID) || !utf8.ValidString(step) {
		return "", apifysettings.ErrInput
	}
	data, err := json.Marshal(struct{ Domain, Generation, JobID, Step string }{"itmap.go431.hh-job.v1", generation, jobID, step})
	if err != nil {
		return "", apifysettings.ErrInput
	}
	sum := sha256.Sum256(data)
	return "go431job:" + hex.EncodeToString(sum[:]), nil
}
