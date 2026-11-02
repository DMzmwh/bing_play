package dao

import (
	"errors"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/bing/bing-play/internal/db/models"
	"github.com/bing/bing-play/internal/filetype"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ResourceDAO 资源聚合读写。
//
// 底层拆成三张表：
//   - resources      资源元数据（InfoHash 为唯一标识）
//   - download_tasks 下载任务状态（InfoHash 为主键）
//   - local_files    本地文件 / 播放项（保留自增 ID 作为播放身份）
//
// 对外仍返回 models.Resource 聚合，读操作按页批量查询三张表后在内存装配，
// 不做 JOIN，也禁止 N+1 查询。
type ResourceDAO struct {
	db *gorm.DB
}

func NewResourceDAO(db *gorm.DB) *ResourceDAO {
	return &ResourceDAO{
		db: db,
	}
}

// WithTx 支持事务
func (d *ResourceDAO) WithTx(tx *gorm.DB) *ResourceDAO {
	return &ResourceDAO{
		db: tx,
	}
}

// Save 写入完整聚合（资源元数据 + 下载任务 + 本地文件）。
// 本地视频没有 InfoHash，自动转交给 SaveLocalResource。
func (d *ResourceDAO) Save(r *models.Resource) error {
	if r == nil {
		return errors.New("resource is nil")
	}
	if r.InfoHash == "" {
		return d.SaveLocalResource(r)
	}

	resource, task, local := models.Split(r)
	return d.db.Transaction(func(tx *gorm.DB) error {
		if err := upsertResourceRecord(tx, resource); err != nil {
			return err
		}
		if err := upsertDownloadTask(tx, task); err != nil {
			return err
		}
		if err := upsertLocalFile(tx, local); err != nil {
			return err
		}
		r.ID = local.ID
		return nil
	})
}

// SaveLocalResource 保存本地视频资源（仅写 local_files）。
// 本地拖入的视频没有 InfoHash，也不参与下载状态机。
func (d *ResourceDAO) SaveLocalResource(r *models.Resource) error {
	if r == nil {
		return errors.New("resource is nil")
	}

	_, _, local := models.Split(r)
	return d.db.Transaction(func(tx *gorm.DB) error {
		if err := upsertLocalFile(tx, local); err != nil {
			return err
		}
		r.ID = local.ID
		return nil
	})
}

// UpdateState 以 CAS 方式写回状态与进度快照。
//
// fromVersion 为本次写入前数据库中期望的 state_version。
// 返回 false 表示版本已变化（存在更新的写入），本次陈旧快照被丢弃。
func (d *ResourceDAO) UpdateState(r *models.Resource, fromVersion uint64) (bool, error) {
	if r == nil || r.InfoHash == "" {
		return false, nil
	}

	now := time.Now()
	updateTime := r.UpdateTime
	if updateTime == nil {
		updateTime = &now
	}

	ok := false
	err := d.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.DownloadTaskRecord{}).
			Where("info_hash = ? AND state_version = ?", r.InfoHash, fromVersion).
			Updates(map[string]any{
				"task_state":       int8(r.TaskState),
				"state_version":    r.StateVersion,
				"attempt":          r.Attempt,
				"max_attempts":     r.MaxAttempts,
				"error_code":       r.ErrorCode,
				"next_retry_at":    r.NextRetryAt,
				"state_changed_at": r.StateChangedAt,
				"priority":         r.Priority,
				"downloaded_size":  r.DownloadedSize,
				"save_path":        r.LocalPath,
				"update_time":      updateTime,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		ok = true

		if err := updateLocalFileByHash(tx, r, updateTime); err != nil {
			return err
		}
		return updateResourceRecordByHash(tx, r, updateTime)
	})
	if err != nil {
		return false, err
	}
	return ok, nil
}

// UpdatePriority 仅更新优先级，不改变状态版本。
func (d *ResourceDAO) UpdatePriority(infoHash string, priority int) error {
	if infoHash == "" {
		return nil
	}
	now := time.Now()
	return d.db.Model(&models.DownloadTaskRecord{}).
		Where("info_hash = ?", infoHash).
		Updates(map[string]any{
			"priority":    priority,
			"update_time": &now,
		}).
		Error
}

// GetByInfoHash 根据 InfoHash 查询资源聚合，不存在时返回 nil。
func (d *ResourceDAO) GetByInfoHash(infoHash string) (*models.Resource, error) {
	if infoHash == "" {
		return nil, nil
	}

	list, err := d.loadByHashes([]string{infoHash})
	if err != nil {
		return nil, err
	}
	return list[infoHash], nil
}

// GetLocalFilesByIDs 按本地文件主键批量查询资源。
func (d *ResourceDAO) GetLocalFilesByIDs(ids []int64) ([]models.Resource, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	var locals []models.LocalFileRecord
	if err := d.db.Where("id IN ?", ids).Order("id ASC").Find(&locals).Error; err != nil {
		return nil, err
	}
	if len(locals) == 0 {
		return nil, nil
	}

	hashes := hashesOfLocals(locals)
	resourceMap, taskMap, err := d.loadResourceAndTask(hashes)
	if err != nil {
		return nil, err
	}

	out := make([]models.Resource, 0, len(locals))
	for i := range locals {
		hash := hashOfLocal(&locals[i])
		out = append(out, *models.Assemble(hash, resourceMap[hash], taskMap[hash], &locals[i]))
	}
	return out, nil
}

// GetByInfoHashes 按 InfoHash 批量查询资源聚合，未命中的哈希不会出现在结果中。
func (d *ResourceDAO) GetByInfoHashes(infoHashes []string) ([]models.Resource, error) {
	if len(infoHashes) == 0 {
		return nil, nil
	}

	byHash, err := d.loadByHashes(infoHashes)
	if err != nil {
		return nil, err
	}

	out := make([]models.Resource, 0, len(infoHashes))
	seen := make(map[string]struct{}, len(infoHashes))
	for _, hash := range infoHashes {
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		if resource, ok := byHash[hash]; ok {
			out = append(out, *resource)
		}
	}
	return out, nil
}

// taskStatesWhere 按状态机的判定条件生成状态切片，避免状态集合在多处重复维护。
func taskStatesWhere(keep func(models.TaskState) bool) []models.TaskState {
	out := make([]models.TaskState, 0, models.TaskStateCount)
	for state := models.TaskState(0); state < models.TaskStateCount; state++ {
		if keep(state) {
			out = append(out, state)
		}
	}
	return out
}

// pendingTaskStates 下载页「待下载」列表状态：用户触发过下载且未完成的任务。
var pendingTaskStates = taskStatesWhere(models.TaskState.Pending)

// downloadTaskStates 下载页需要展示的任务状态，Discovered 不在其中。
var downloadTaskStates = taskStatesWhere(func(state models.TaskState) bool {
	return state.Pending() || state == models.TaskCompleted
})

// int8Slice 把强类型状态枚举转成驱动可直接绑定的切片。
func int8Slice[T ~int8](values []T) []int8 {
	out := make([]int8, len(values))
	for i, v := range values {
		out[i] = int8(v)
	}
	return out
}

// ListDownloadTasks 查询全部种子下载任务，按更新时间倒序。
func (d *ResourceDAO) ListDownloadTasks() ([]models.Resource, error) {
	return d.listTasksWhere(nil, "update_time DESC")
}

// ListPendingTasks 查询下载页「待下载」列表的全部任务。
func (d *ResourceDAO) ListPendingTasks() ([]models.Resource, error) {
	return d.ListByTaskStates(pendingTaskStates)
}

// ListCompletedTasks 查询已完成的种子下载任务。
func (d *ResourceDAO) ListCompletedTasks() ([]models.Resource, error) {
	return d.listTasksWhere([]models.TaskState{models.TaskCompleted}, "update_time DESC")
}

// ListByTaskStates 按任务状态查询资源，供启动时恢复未完成任务。
func (d *ResourceDAO) ListByTaskStates(states []models.TaskState) ([]models.Resource, error) {
	if len(states) == 0 {
		return nil, nil
	}
	return d.listTasksWhere(states, "")
}

// listTasksWhere 查询任务表并按 hash 批量补齐资源与本地文件。
func (d *ResourceDAO) listTasksWhere(states []models.TaskState, order string) ([]models.Resource, error) {
	var tasks []models.DownloadTaskRecord
	query := d.db.Model(&models.DownloadTaskRecord{})
	if states == nil {
		query = query.Where("task_state IN ?", int8Slice(downloadTaskStates))
	} else {
		query = query.Where("task_state IN ?", int8Slice(states))
	}
	if order != "" {
		query = query.Order(order)
	}
	if err := query.Find(&tasks).Error; err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return nil, nil
	}

	hashes := make([]string, 0, len(tasks))
	for i := range tasks {
		hashes = append(hashes, tasks[i].InfoHash)
	}
	resourceMap, _, localMap, err := d.loadAll(hashes)
	if err != nil {
		return nil, err
	}

	out := make([]models.Resource, 0, len(tasks))
	for i := range tasks {
		task := &tasks[i]
		hash := task.InfoHash
		out = append(out, *models.Assemble(hash, resourceMap[hash], task, localMap[hash]))
	}
	return out, nil
}

// DeleteLocalFilesByIDs 按本地文件主键批量删除记录，不删除磁盘文件。
// 仅用于用户拖入的本地视频，种子资源应重置为未下载状态。
func (d *ResourceDAO) DeleteLocalFilesByIDs(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return d.db.Where("id IN ?", ids).Delete(&models.LocalFileRecord{}).Error
}

// ResetToUndownloadedByHashes 按 InfoHash 批量把种子资源重置为「未下载」，
// 清理下载进度与本地文件关联。磁盘上的文件不会删除。
func (d *ResourceDAO) ResetToUndownloadedByHashes(infoHashes []string, now time.Time) error {
	if len(infoHashes) == 0 {
		return nil
	}

	states := []models.TaskState{models.TaskDownloading, models.TaskVerifying, models.TaskCompleted}
	return d.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.DownloadTaskRecord{}).
			Where("info_hash IN ?", infoHashes).
			Where("task_state IN ?", int8Slice(states)).
			Updates(map[string]any{
				"task_state":       int8(models.TaskDiscovered),
				"state_version":    gorm.Expr("state_version + 1"),
				"attempt":          0,
				"error_code":       "",
				"next_retry_at":    nil,
				"state_changed_at": now,
				"downloaded_size":  nil,
				"save_path":        "",
				"update_time":      now,
			}).Error; err != nil {
			return err
		}

		return tx.Model(&models.LocalFileRecord{}).
			Where("info_hash IN ?", infoHashes).
			Updates(map[string]any{
				"local_state":       int8(models.LocalNone),
				"local_loss_reason": int8(models.LossNone),
				"file_name":         nil,
				"file_size":         nil,
				"mime_type":         nil,
				"local_path":        "",
				"verified_at":       nil,
				"local_checked_at":  nil,
				"update_time":       now,
			}).Error
	})
}

// GetLocalFile 根据本地文件 ID 查询资源聚合。
func (d *ResourceDAO) GetLocalFile(id string) (*models.Resource, error) {
	var local models.LocalFileRecord
	err := d.db.Where("id = ?", id).First(&local).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	hash := hashOfLocal(&local)
	if hash == "" {
		return models.Assemble("", nil, nil, &local), nil
	}

	resourceMap, taskMap, err := d.loadResourceAndTask([]string{hash})
	if err != nil {
		return nil, err
	}
	return models.Assemble(hash, resourceMap[hash], taskMap[hash], &local), nil
}

// ListPlayable 查询播放列表所需的资源，按资源名称、创建时间排序。
//
// 返回三类记录：
//   - 本地视频：local_files.info_hash 为 NULL 且文件齐全；
//   - 已完成种子：download_tasks 完成且本地文件齐全；
//   - 下载中 / 校验中的种子：支持边下边播。
func (d *ResourceDAO) ListPlayable() ([]models.Resource, error) {
	var active []models.DownloadTaskRecord
	if err := d.db.Select("info_hash").
		Where("task_state IN ?", int8Slice([]models.TaskState{models.TaskDownloading, models.TaskVerifying})).
		Find(&active).Error; err != nil {
		return nil, err
	}
	activeSet := make(map[string]struct{}, len(active))
	activeHashes := make([]string, 0, len(active))
	for i := range active {
		hash := active[i].InfoHash
		activeSet[hash] = struct{}{}
		activeHashes = append(activeHashes, hash)
	}

	where := "(info_hash IS NULL AND local_state = ?) OR (info_hash IS NOT NULL AND local_state = ?)"
	args := []any{int8(models.LocalPresent), int8(models.LocalPresent)}
	if len(activeHashes) > 0 {
		where += " OR info_hash IN ?"
		args = append(args, activeHashes)
	}

	var locals []models.LocalFileRecord
	if err := d.db.Where("local_path <> ''").Where(where, args...).Find(&locals).Error; err != nil {
		return nil, err
	}
	if len(locals) == 0 {
		return nil, nil
	}

	hashes := hashesOfLocals(locals)
	resourceMap, taskMap, err := d.loadResourceAndTask(hashes)
	if err != nil {
		return nil, err
	}

	playable := make([]models.Resource, 0, len(locals))
	for i := range locals {
		hash := hashOfLocal(&locals[i])
		if hash != "" {
			task := taskMap[hash]
			if task == nil {
				continue
			}
			_, activeTask := activeSet[hash]
			if task.TaskState != models.TaskCompleted && !activeTask {
				continue
			}
		}
		resource := models.Assemble(hash, resourceMap[hash], taskMap[hash], &locals[i])
		if resource.Kind == models.KindTorrent {
			if resource.FileName == nil || !filetype.IsVideo(*resource.FileName) {
				continue
			}
		}
		playable = append(playable, *resource)
	}

	sort.SliceStable(playable, func(i, j int) bool {
		left, right := resourceNameOf(&playable[i]), resourceNameOf(&playable[j])
		if left != right {
			return left < right
		}
		return createTimeOf(&playable[i]).Before(createTimeOf(&playable[j]))
	})

	return playable, nil
}

// SaveCrawledResources 批量写入爬虫抓取到的种子资源，仅写 resources 表。
// 依靠 info_hash 主键做去重：已存在的记录直接跳过。
func (d *ResourceDAO) SaveCrawledResources(list []*models.Resource) error {
	if len(list) == 0 {
		return nil
	}

	rows := make([]*models.ResourceRecord, 0, len(list))
	for _, resource := range list {
		record, _, _ := models.Split(resource)
		if record == nil || record.InfoHash == "" {
			continue
		}
		rows = append(rows, record)
	}
	if len(rows) == 0 {
		return nil
	}

	return d.db.
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(rows, 100).
		Error
}

// CrawledListQuery 已爬取资源列表的查询条件。
type CrawledListQuery struct {
	// Keyword 按资源名称或 InfoHash 模糊匹配。
	Keyword string
	// SortBy 排序字段：create_time / total_size / resource_name。
	SortBy string
	// SortOrder 排序方向：asc / desc。
	SortOrder string
	// Offset 跳过条数。
	Offset int
	// Limit 返回条数。
	Limit int
}

// crawledSortColumns 允许的排序字段白名单，避免拼接到 SQL 时被注入。
var crawledSortColumns = map[string]string{
	"create_time":   "create_time",
	"total_size":    "total_size",
	"resource_name": "resource_name",
}

// ListCrawled 分页查询爬虫抓取到的资源。
// 只返回带 InfoHash 的目录记录；本地视频不在 resources 表中。
func (d *ResourceDAO) ListCrawled(q CrawledListQuery) ([]models.Resource, int64, error) {
	base := d.db.Model(&models.ResourceRecord{}).Where("info_hash IS NOT NULL AND info_hash <> ''")

	if keyword := strings.TrimSpace(q.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		base = base.Where("resource_name LIKE ? OR info_hash LIKE ?", like, like)
	}

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	column, ok := crawledSortColumns[q.SortBy]
	if !ok {
		column = "create_time"
	}
	desc := q.SortOrder != "asc"

	limit := q.Limit
	if limit <= 0 {
		limit = 20
	}

	var records []models.ResourceRecord
	err := base.Session(&gorm.Session{}).
		Order(clause.OrderByColumn{Column: clause.Column{Name: column}, Desc: desc}).
		Offset(q.Offset).
		Limit(limit).
		Find(&records).
		Error
	if err != nil {
		return nil, 0, err
	}
	if len(records) == 0 {
		return nil, total, nil
	}

	hashes := make([]string, 0, len(records))
	for i := range records {
		hashes = append(hashes, records[i].InfoHash)
	}
	taskMap, localMap, err := d.loadTaskAndLocal(hashes)
	if err != nil {
		return nil, 0, err
	}

	list := make([]models.Resource, 0, len(records))
	for i := range records {
		record := &records[i]
		hash := record.InfoHash
		list = append(list, *models.Assemble(hash, record, taskMap[hash], localMap[hash]))
	}
	return list, total, nil
}

// FindByLocalFile 按「文件所在目录 + 文件名」查询已登记的资源。
//
// local_path 统一保存文件所在目录，file_name 保存文件名（可带子目录）；
// 只匹配当前可播放的记录：本地视频，或已完成且本地齐全的种子。
// 同一文件有多条记录时优先返回种子记录。
func (d *ResourceDAO) FindByLocalFile(dir, fileName string) (*models.Resource, error) {
	var locals []models.LocalFileRecord
	err := d.db.
		Where("local_path <> ''").
		Where("info_hash IS NULL OR (info_hash IS NOT NULL AND local_state = ?)", int8(models.LocalPresent)).
		Order("id ASC").
		Find(&locals).
		Error
	if err != nil {
		return nil, err
	}
	if len(locals) == 0 {
		return nil, nil
	}

	hashes := hashesOfLocals(locals)
	resourceMap, taskMap, err := d.loadResourceAndTask(hashes)
	if err != nil {
		return nil, err
	}

	candidates := make([]models.Resource, 0, len(locals))
	for i := range locals {
		hash := hashOfLocal(&locals[i])
		if hash != "" {
			task := taskMap[hash]
			if task == nil || task.TaskState != models.TaskCompleted {
				continue
			}
		}
		candidates = append(candidates, *models.Assemble(hash, resourceMap[hash], taskMap[hash], &locals[i]))
	}

	// 种子记录优先，其次按 ID 升序，保证同一文件有重复登记时复用下载任务数据。
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Kind != candidates[j].Kind {
			return candidates[i].Kind < candidates[j].Kind
		}
		return candidates[i].ID < candidates[j].ID
	})

	target := normalizeLocalPath(filepath.Join(dir, fileName))
	targetName := filepath.Base(target)
	for i := range candidates {
		resource := &candidates[i]
		if resource.FileName == nil || !sameFileName(filepath.Base(*resource.FileName), targetName) {
			continue
		}
		if normalizeLocalPath(filepath.Join(resource.LocalPath, *resource.FileName)) == target {
			return resource, nil
		}
	}

	return nil, nil
}

// normalizeLocalPath 把路径转为绝对路径并清理，Windows 下统一小写以便忽略大小写差异。
func normalizeLocalPath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// sameFileName 比较两个文件名，Windows 下忽略大小写。
func sameFileName(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// =========================================================
// 三表读写辅助
// =========================================================

func upsertResourceRecord(tx *gorm.DB, record *models.ResourceRecord) error {
	if record == nil || record.InfoHash == "" {
		return nil
	}
	now := time.Now()
	if record.CreateTime == nil {
		record.CreateTime = &now
	}
	if record.UpdateTime == nil {
		record.UpdateTime = &now
	}

	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "info_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"resource_name", "source_url", "cover_url", "total_size",
			"torrent_path", "resource_type", "favorite", "tags", "remark", "update_time",
		}),
	}).Create(record).Error
}

func upsertDownloadTask(tx *gorm.DB, record *models.DownloadTaskRecord) error {
	if record == nil {
		return nil
	}
	now := time.Now()
	if record.UpdateTime == nil {
		record.UpdateTime = &now
	}

	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "info_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "source_url", "torrent_path", "total_size",
			"task_state", "state_version", "attempt", "max_attempts",
			"error_code", "next_retry_at", "state_changed_at",
			"priority", "downloaded_size", "save_path", "update_time",
		}),
	}).Create(record).Error
}

func upsertLocalFile(tx *gorm.DB, record *models.LocalFileRecord) error {
	if record == nil {
		return nil
	}
	now := time.Now()
	if record.UpdateTime == nil {
		record.UpdateTime = &now
	}

	if record.ID != 0 {
		result := tx.Model(&models.LocalFileRecord{}).
			Where("id = ?", record.ID).
			Updates(map[string]any{
				"info_hash":         record.InfoHash,
				"resource_name":     record.ResourceName,
				"local_state":       int8(record.LocalState),
				"local_loss_reason": int8(record.LocalLossReason),
				"local_path":        record.LocalPath,
				"file_name":         record.FileName,
				"file_size":         record.FileSize,
				"mime_type":         record.MimeType,
				"verified_at":       record.VerifiedAt,
				"local_checked_at":  record.LocalCheckedAt,
				"total_duration":    record.TotalDuration,
				"played":            record.Played,
				"play_progress":     record.PlayProgress,
				"play_count":        record.PlayCount,
				"last_play_time":    record.LastPlayTime,
				"finished":          record.Finished,
				"update_time":       record.UpdateTime,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}
		record.ID = 0
	}

	if record.CreateTime == nil {
		record.CreateTime = &now
	}
	return tx.Create(record).Error
}

// updateLocalFileByHash 更新任务对应的本地文件行，缺失时补建。
func updateLocalFileByHash(tx *gorm.DB, r *models.Resource, updateTime *time.Time) error {
	result := tx.Model(&models.LocalFileRecord{}).
		Where("info_hash = ?", r.InfoHash).
		Updates(map[string]any{
			"local_state":       int8(r.LocalState),
			"local_loss_reason": int8(r.LocalLossReason),
			"local_path":        r.LocalPath,
			"file_name":         r.FileName,
			"file_size":         r.FileSize,
			"mime_type":         r.MimeType,
			"verified_at":       r.VerifiedAt,
			"local_checked_at":  r.LocalCheckedAt,
			"update_time":       updateTime,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	_, _, local := models.Split(r)
	local.ID = 0
	if local.CreateTime == nil {
		local.CreateTime = updateTime
	}
	return tx.Create(local).Error
}

// updateResourceRecordByHash 更新任务对应的资源元数据，缺失时补建。
func updateResourceRecordByHash(tx *gorm.DB, r *models.Resource, updateTime *time.Time) error {
	result := tx.Model(&models.ResourceRecord{}).
		Where("info_hash = ?", r.InfoHash).
		Updates(map[string]any{
			"resource_name": r.ResourceName,
			"source_url":    r.SourceURL,
			"torrent_path":  r.TorrentPath,
			"total_size":    r.TotalSize,
			"resource_type": r.ResourceType,
			"update_time":   updateTime,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	record, _, _ := models.Split(r)
	if record.CreateTime == nil {
		record.CreateTime = updateTime
	}
	return tx.Create(record).Error
}

// loadByHashes 按 hash 查询三表并装配；结果只包含至少存在一行数据的哈希。
func (d *ResourceDAO) loadByHashes(infoHashes []string) (map[string]*models.Resource, error) {
	hashes := uniqueNonEmpty(infoHashes)
	if len(hashes) == 0 {
		return nil, nil
	}

	resourceMap, taskMap, localMap, err := d.loadAll(hashes)
	if err != nil {
		return nil, err
	}

	out := make(map[string]*models.Resource, len(hashes))
	for _, hash := range hashes {
		if resourceMap[hash] == nil && taskMap[hash] == nil && localMap[hash] == nil {
			continue
		}
		out[hash] = models.Assemble(hash, resourceMap[hash], taskMap[hash], localMap[hash])
	}
	return out, nil
}

// loadAll 批量读取三张表。
func (d *ResourceDAO) loadAll(infoHashes []string) (
	map[string]*models.ResourceRecord,
	map[string]*models.DownloadTaskRecord,
	map[string]*models.LocalFileRecord,
	error,
) {
	resourceMap, err := d.loadResources(infoHashes)
	if err != nil {
		return nil, nil, nil, err
	}
	taskMap, err := d.loadTasks(infoHashes)
	if err != nil {
		return nil, nil, nil, err
	}
	localMap, err := d.loadLocals(infoHashes)
	if err != nil {
		return nil, nil, nil, err
	}
	return resourceMap, taskMap, localMap, nil
}

// loadResourceAndTask 只读取资源元数据与下载任务，本地文件由调用方提供。
func (d *ResourceDAO) loadResourceAndTask(infoHashes []string) (
	map[string]*models.ResourceRecord,
	map[string]*models.DownloadTaskRecord,
	error,
) {
	resourceMap, err := d.loadResources(infoHashes)
	if err != nil {
		return nil, nil, err
	}
	taskMap, err := d.loadTasks(infoHashes)
	if err != nil {
		return nil, nil, err
	}
	return resourceMap, taskMap, nil
}

// loadTaskAndLocal 只读取下载任务与本地文件，资源元数据由调用方提供。
func (d *ResourceDAO) loadTaskAndLocal(infoHashes []string) (
	map[string]*models.DownloadTaskRecord,
	map[string]*models.LocalFileRecord,
	error,
) {
	taskMap, err := d.loadTasks(infoHashes)
	if err != nil {
		return nil, nil, err
	}
	localMap, err := d.loadLocals(infoHashes)
	if err != nil {
		return nil, nil, err
	}
	return taskMap, localMap, nil
}

func (d *ResourceDAO) loadResources(infoHashes []string) (map[string]*models.ResourceRecord, error) {
	hashes := uniqueNonEmpty(infoHashes)
	if len(hashes) == 0 {
		return nil, nil
	}

	var records []models.ResourceRecord
	if err := d.db.Where("info_hash IN ?", hashes).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make(map[string]*models.ResourceRecord, len(records))
	for i := range records {
		out[records[i].InfoHash] = &records[i]
	}
	return out, nil
}

func (d *ResourceDAO) loadTasks(infoHashes []string) (map[string]*models.DownloadTaskRecord, error) {
	hashes := uniqueNonEmpty(infoHashes)
	if len(hashes) == 0 {
		return nil, nil
	}

	var records []models.DownloadTaskRecord
	if err := d.db.Where("info_hash IN ?", hashes).Find(&records).Error; err != nil {
		return nil, err
	}
	out := make(map[string]*models.DownloadTaskRecord, len(records))
	for i := range records {
		out[records[i].InfoHash] = &records[i]
	}
	return out, nil
}

func (d *ResourceDAO) loadLocals(infoHashes []string) (map[string]*models.LocalFileRecord, error) {
	hashes := uniqueNonEmpty(infoHashes)
	if len(hashes) == 0 {
		return nil, nil
	}

	var records []models.LocalFileRecord
	if err := d.db.Where("info_hash IN ?", hashes).Order("id ASC").Find(&records).Error; err != nil {
		return nil, err
	}
	out := make(map[string]*models.LocalFileRecord, len(records))
	for i := range records {
		hash := hashOfLocal(&records[i])
		if hash == "" {
			continue
		}
		if _, exists := out[hash]; exists {
			continue
		}
		out[hash] = &records[i]
	}
	return out, nil
}

// uniqueNonEmpty 去重并过滤空 InfoHash。
func uniqueNonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func hashOfLocal(local *models.LocalFileRecord) string {
	if local == nil || local.InfoHash == nil {
		return ""
	}
	return *local.InfoHash
}

func hashesOfLocals(locals []models.LocalFileRecord) []string {
	out := make([]string, 0, len(locals))
	for i := range locals {
		if hash := hashOfLocal(&locals[i]); hash != "" {
			out = append(out, hash)
		}
	}
	return out
}

func resourceNameOf(resource *models.Resource) string {
	if resource == nil || resource.ResourceName == nil {
		return ""
	}
	return *resource.ResourceName
}

func createTimeOf(resource *models.Resource) time.Time {
	if resource == nil || resource.CreateTime == nil {
		return time.Time{}
	}
	return *resource.CreateTime
}
