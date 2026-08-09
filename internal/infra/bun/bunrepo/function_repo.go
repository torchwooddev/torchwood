package bunrepo

import (
	"context"
	"database/sql"
	"errors"
	"time"

	domainfunctions "github.com/torchwooddev/torchwood/internal/domain/functions"
	"github.com/torchwooddev/torchwood/internal/infra/bun/model"
	"github.com/torchwooddev/torchwood/internal/infra/clients"
	"github.com/uptrace/bun"
)

type functionRepo struct {
	db *clients.Database
}

func NewFunctionRepository(db *clients.Database) domainfunctions.FunctionRepo {
	return &functionRepo{db: db}
}

func (r *functionRepo) CreateFunction(ctx context.Context, fn *domainfunctions.Function) error {
	m := mapFunctionToModel(fn)
	_, err := r.db.NewInsert().Model(m).Exec(ctx)
	return err
}

func (r *functionRepo) GetFunction(ctx context.Context, projectID, functionID string) (*domainfunctions.Function, error) {
	m := new(model.Function)
	err := r.db.NewSelect().Model(m).
		Where("f.project_id = ?", projectID).
		Where("f.id = ?", functionID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapFunctionToDomain(m), nil
}

func (r *functionRepo) ListFunctions(ctx context.Context, projectID string) ([]domainfunctions.Function, error) {
	var ms []model.Function
	err := r.db.NewSelect().Model(&ms).
		Where("project_id = ?", projectID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domainfunctions.Function, len(ms))
	for i := range ms {
		out[i] = *mapFunctionToDomain(&ms[i])
	}
	return out, nil
}

func (r *functionRepo) UpdateFunction(ctx context.Context, fn *domainfunctions.Function) error {
	m := mapFunctionToModel(fn)
	_, err := r.db.NewUpdate().Model(m).WherePK().Exec(ctx)
	return err
}

func (r *functionRepo) DeleteFunction(ctx context.Context, projectID, functionID string) error {
	_, err := r.db.NewDelete().Model((*model.Function)(nil)).
		Where("project_id = ?", projectID).
		Where("id = ?", functionID).
		Exec(ctx)
	return err
}

func (r *functionRepo) CreateDeployment(ctx context.Context, d *domainfunctions.Deployment) error {
	m := mapDeploymentToModel(d)
	_, err := r.db.NewInsert().Model(m).Exec(ctx)
	return err
}

func (r *functionRepo) GetDeployment(ctx context.Context, functionID, deploymentID string) (*domainfunctions.Deployment, error) {
	m := new(model.FunctionDeployment)
	err := r.db.NewSelect().Model(m).
		Where("fd.function_id = ?", functionID).
		Where("fd.id = ?", deploymentID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapDeploymentToDomain(m), nil
}

func (r *functionRepo) ListDeployments(ctx context.Context, projectID, functionID string) ([]domainfunctions.Deployment, error) {
	var ms []model.FunctionDeployment
	err := r.db.NewSelect().Model(&ms).
		Where("fd.project_id = ?", projectID).
		Where("fd.function_id = ?", functionID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domainfunctions.Deployment, len(ms))
	for i := range ms {
		out[i] = *mapDeploymentToDomain(&ms[i])
	}
	return out, nil
}

func (r *functionRepo) UpdateDeployment(ctx context.Context, d *domainfunctions.Deployment) error {
	m := mapDeploymentToModel(d)
	_, err := r.db.NewUpdate().Model(m).WherePK().Exec(ctx)
	return err
}

func (r *functionRepo) DeleteDeployment(ctx context.Context, functionID, deploymentID string) error {
	_, err := r.db.NewDelete().Model((*model.FunctionDeployment)(nil)).
		Where("function_id = ?", functionID).
		Where("id = ?", deploymentID).
		Exec(ctx)
	return err
}

func (r *functionRepo) SetVariables(ctx context.Context, projectID, functionID string, vars map[string]string) error {
	tx, err := r.db.Conn(ctx).BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.NewDelete().Model((*model.FunctionVariable)(nil)).
		Where("function_id = ?", functionID).
		Exec(ctx); err != nil {
		return err
	}

	for k, v := range vars {
		if _, err := tx.NewInsert().Model(&model.FunctionVariable{
			ID:         "var_" + functionID + "_" + k,
			FunctionID: functionID,
			ProjectID:  projectID,
			Key:        k,
			Value:      v,
		}).Exec(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *functionRepo) GetVariables(ctx context.Context, projectID, functionID string) (map[string]string, error) {
	var ms []model.FunctionVariable
	err := r.db.NewSelect().Model(&ms).
		Where("fv.project_id = ?", projectID).
		Where("fv.function_id = ?", functionID).
		Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(ms))
	for i := range ms {
		out[ms[i].Key] = ms[i].Value
	}
	return out, nil
}

func (r *functionRepo) CreateExecution(ctx context.Context, e *domainfunctions.ExecutionRecord) error {
	m := mapExecutionToModel(e)
	_, err := r.db.NewInsert().Model(m).Exec(ctx)
	return err
}

func (r *functionRepo) GetExecution(ctx context.Context, projectID, functionID, executionID string) (*domainfunctions.ExecutionRecord, error) {
	m := new(model.FunctionExecution)
	err := r.db.NewSelect().Model(m).
		Where("fe.project_id = ?", projectID).
		Where("fe.function_id = ?", functionID).
		Where("fe.id = ?", executionID).
		Scan(ctx)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return mapExecutionToDomain(m), nil
}

func (r *functionRepo) ListExecutions(ctx context.Context, projectID, functionID string, limit int) ([]domainfunctions.ExecutionRecord, error) {
	var ms []model.FunctionExecution
	q := r.db.NewSelect().Model(&ms).
		Where("fe.project_id = ?", projectID).
		Where("fe.function_id = ?", functionID).
		Order("created_at DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Scan(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]domainfunctions.ExecutionRecord, len(ms))
	for i := range ms {
		out[i] = *mapExecutionToDomain(&ms[i])
	}
	return out, nil
}

func (r *functionRepo) UpdateExecution(ctx context.Context, e *domainfunctions.ExecutionRecord) error {
	m := mapExecutionToModel(e)
	_, err := r.db.NewUpdate().Model(m).WherePK().Exec(ctx)
	return err
}

func (r *functionRepo) RecoverOrphanExecutions(ctx context.Context, staleAfter time.Duration) (int64, error) {
	cutoff := time.Now().Add(-staleAfter)
	res, err := r.db.NewUpdate().Model((*model.FunctionExecution)(nil)).
		Set("status = ?", domainfunctions.ExecutionStatusFailed).
		Set("error = ?", "worker restarted").
		Set("updated_at = NOW()").
		Where("status IN (?)", bun.In([]string{
			domainfunctions.ExecutionStatusQueued,
			domainfunctions.ExecutionStatusBuilding,
			domainfunctions.ExecutionStatusRunning,
		})).
		Where("updated_at < ?", cutoff).
		Exec(ctx)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (r *functionRepo) PruneOldExecutions(ctx context.Context, functionID string, keepRecent int) error {
	sub := r.db.NewSelect().Model((*model.FunctionExecution)(nil)).
		Column("id").
		Where("function_id = ?", functionID).
		Order("created_at DESC").
		Limit(keepRecent)
	_, err := r.db.NewDelete().Model((*model.FunctionExecution)(nil)).
		Where("function_id = ?", functionID).
		Where("id NOT IN (?)", sub).
		Exec(ctx)
	return err
}

func mapFunctionToModel(fn *domainfunctions.Function) *model.Function {
	return &model.Function{
		ID:             fn.ID,
		ProjectID:      fn.ProjectID,
		Name:           fn.Name,
		Runtime:        fn.Runtime,
		Entrypoint:     fn.Entrypoint,
		TimeoutSeconds: fn.TimeoutSeconds,
		Spec:           fn.Spec,
		Enabled:        fn.Enabled,
		CreatedAt:      fn.CreatedAt,
		UpdatedAt:      fn.UpdatedAt,
	}
}

func mapFunctionToDomain(m *model.Function) *domainfunctions.Function {
	return &domainfunctions.Function{
		ID:             m.ID,
		ProjectID:      m.ProjectID,
		Name:           m.Name,
		Runtime:        m.Runtime,
		Entrypoint:     m.Entrypoint,
		TimeoutSeconds: m.TimeoutSeconds,
		Spec:           m.Spec,
		Enabled:        m.Enabled,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

func mapDeploymentToModel(d *domainfunctions.Deployment) *model.FunctionDeployment {
	return &model.FunctionDeployment{
		ID:         d.ID,
		FunctionID: d.FunctionID,
		ProjectID:  d.ProjectID,
		Size:       d.Size,
		Status:     d.Status,
		Error:      d.Error,
		CreatedAt:  d.CreatedAt,
		UpdatedAt:  d.UpdatedAt,
	}
}

func mapDeploymentToDomain(m *model.FunctionDeployment) *domainfunctions.Deployment {
	return &domainfunctions.Deployment{
		ID:         m.ID,
		FunctionID: m.FunctionID,
		ProjectID:  m.ProjectID,
		Size:       m.Size,
		Status:     m.Status,
		Error:      m.Error,
		CreatedAt:  m.CreatedAt,
		UpdatedAt:  m.UpdatedAt,
	}
}

func mapExecutionToModel(e *domainfunctions.ExecutionRecord) *model.FunctionExecution {
	return &model.FunctionExecution{
		ID:                e.ID,
		FunctionID:        e.FunctionID,
		ProjectID:         e.ProjectID,
		DeploymentID:      e.DeploymentID,
		Status:            e.Status,
		Response:          e.Response,
		ResponseTruncated: e.ResponseTruncated,
		Stdout:            e.Stdout,
		StdoutTruncated:   e.StdoutTruncated,
		Stderr:            e.Stderr,
		StderrTruncated:   e.StderrTruncated,
		StatusCode:        e.StatusCode,
		DurationMS:        e.DurationMS,
		Error:             e.Error,
		CreatedAt:         e.CreatedAt,
		UpdatedAt:         e.UpdatedAt,
	}
}

func mapExecutionToDomain(m *model.FunctionExecution) *domainfunctions.ExecutionRecord {
	return &domainfunctions.ExecutionRecord{
		ID:                m.ID,
		FunctionID:        m.FunctionID,
		ProjectID:         m.ProjectID,
		DeploymentID:      m.DeploymentID,
		Status:            m.Status,
		Response:          m.Response,
		ResponseTruncated: m.ResponseTruncated,
		Stdout:            m.Stdout,
		StdoutTruncated:   m.StdoutTruncated,
		Stderr:            m.Stderr,
		StderrTruncated:   m.StderrTruncated,
		StatusCode:        m.StatusCode,
		DurationMS:        m.DurationMS,
		Error:             m.Error,
		CreatedAt:         m.CreatedAt,
		UpdatedAt:         m.UpdatedAt,
	}
}
