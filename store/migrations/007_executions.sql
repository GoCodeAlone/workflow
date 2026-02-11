-- Execution tracking tables
CREATE TABLE workflow_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    trigger_type TEXT NOT NULL,
    trigger_data JSONB,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'cancelled')),
    output_data JSONB,
    error_message TEXT,
    error_stack TEXT,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    duration_ms BIGINT,
    metadata JSONB DEFAULT '{}'
);
CREATE INDEX idx_executions_workflow ON workflow_executions(workflow_id);
CREATE INDEX idx_executions_status ON workflow_executions(status);
CREATE INDEX idx_executions_started ON workflow_executions(started_at DESC);

CREATE TABLE execution_steps (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    execution_id UUID NOT NULL REFERENCES workflow_executions(id) ON DELETE CASCADE,
    step_name TEXT NOT NULL,
    step_type TEXT NOT NULL,
    input_data JSONB,
    output_data JSONB,
    status TEXT NOT NULL CHECK (status IN ('pending', 'running', 'completed', 'failed', 'skipped')),
    error_message TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    duration_ms BIGINT,
    sequence_num INT NOT NULL,
    metadata JSONB DEFAULT '{}'
);
CREATE INDEX idx_steps_execution ON execution_steps(execution_id);
