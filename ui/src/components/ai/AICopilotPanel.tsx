import { useState } from 'react';
import useWorkflowStore from '../../store/workflowStore.ts';
import { generateWorkflow, suggestWorkflows } from '../../utils/api.ts';
import type { WorkflowConfig } from '../../types/workflow.ts';
import type { WorkflowSuggestion } from '../../utils/api.ts';

const QUICK_SUGGESTIONS = [
  'REST API with auth and rate limiting',
  'Event-driven microservice',
  'HTTP proxy with logging',
  'Scheduled data pipeline',
  'WebSocket chat backend',
];

export default function AICopilotPanel() {
  const [intent, setIntent] = useState('');
  const [loading, setLoading] = useState(false);
  const [generatedConfig, setGeneratedConfig] = useState<WorkflowConfig | null>(null);
  const [explanation, setExplanation] = useState('');
  const [suggestions, setSuggestions] = useState<WorkflowSuggestion[]>([]);
  const [suggestLoading, setSuggestLoading] = useState(false);

  const importFromConfig = useWorkflowStore((s) => s.importFromConfig);
  const addToast = useWorkflowStore((s) => s.addToast);
  const toggleAIPanel = useWorkflowStore((s) => s.toggleAIPanel);

  const handleGenerate = async () => {
    if (!intent.trim()) return;
    setLoading(true);
    setGeneratedConfig(null);
    setExplanation('');
    try {
      const result = await generateWorkflow(intent.trim());
      setGeneratedConfig(result.config);
      setExplanation(result.explanation);
    } catch (e) {
      addToast(`Generation failed: ${(e as Error).message}`, 'error');
    } finally {
      setLoading(false);
    }
  };

  const handleApply = () => {
    if (!generatedConfig) return;
    importFromConfig(generatedConfig);
    addToast('Generated workflow applied to canvas', 'success');
    setGeneratedConfig(null);
    setExplanation('');
    setIntent('');
  };

  const handleSuggest = async (useCase: string) => {
    setSuggestLoading(true);
    try {
      const result = await suggestWorkflows(useCase);
      setSuggestions(result);
    } catch (e) {
      addToast(`Suggestions failed: ${(e as Error).message}`, 'error');
    } finally {
      setSuggestLoading(false);
    }
  };

  const handleQuickGenerate = (text: string) => {
    setIntent(text);
    setGeneratedConfig(null);
    setExplanation('');
  };

  return (
    <div
      style={{
        width: 360,
        background: '#181825',
        borderLeft: '1px solid #313244',
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div
        style={{
          padding: '12px 16px',
          borderBottom: '1px solid #313244',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <span style={{ fontWeight: 700, fontSize: 14, color: '#cdd6f4' }}>AI Copilot</span>
        <button
          onClick={toggleAIPanel}
          style={{
            background: 'none',
            border: 'none',
            color: '#585b70',
            cursor: 'pointer',
            fontSize: 16,
            padding: '0 4px',
          }}
        >
          x
        </button>
      </div>

      <div style={{ flex: 1, overflowY: 'auto', padding: 16 }}>
        {/* Input */}
        <div style={{ marginBottom: 16 }}>
          <label style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 6 }}>
            Describe your workflow
          </label>
          <textarea
            value={intent}
            onChange={(e) => setIntent(e.target.value)}
            placeholder="e.g., REST API with JWT authentication, rate limiting, and database storage"
            rows={4}
            style={{
              width: '100%',
              padding: '8px 10px',
              background: '#1e1e2e',
              border: '1px solid #313244',
              borderRadius: 6,
              color: '#cdd6f4',
              fontSize: 12,
              resize: 'vertical',
              outline: 'none',
              fontFamily: 'system-ui, sans-serif',
              boxSizing: 'border-box',
            }}
            onKeyDown={(e) => {
              if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
                handleGenerate();
              }
            }}
          />
          <button
            onClick={handleGenerate}
            disabled={loading || !intent.trim()}
            style={{
              width: '100%',
              padding: '8px 14px',
              background: loading ? '#45475a' : '#89b4fa',
              border: 'none',
              borderRadius: 6,
              color: loading ? '#a6adc8' : '#1e1e2e',
              fontSize: 12,
              fontWeight: 600,
              cursor: loading ? 'default' : 'pointer',
              marginTop: 8,
            }}
          >
            {loading ? 'Generating...' : 'Generate Workflow'}
          </button>
        </div>

        {/* Quick suggestions */}
        <div style={{ marginBottom: 16 }}>
          <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 8 }}>
            Quick start
          </span>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
            {QUICK_SUGGESTIONS.map((s) => (
              <button
                key={s}
                onClick={() => handleQuickGenerate(s)}
                style={{
                  padding: '4px 10px',
                  background: '#1e1e2e',
                  border: '1px solid #313244',
                  borderRadius: 12,
                  color: '#89b4fa',
                  fontSize: 11,
                  cursor: 'pointer',
                  whiteSpace: 'nowrap',
                }}
              >
                {s}
              </button>
            ))}
          </div>
        </div>

        {/* Generated result */}
        {generatedConfig && (
          <div style={{ marginBottom: 16 }}>
            <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 6 }}>
              Generated workflow
            </span>
            {explanation && (
              <p style={{ color: '#bac2de', fontSize: 12, lineHeight: 1.5, marginBottom: 8 }}>
                {explanation}
              </p>
            )}
            <div
              style={{
                background: '#1e1e2e',
                border: '1px solid #313244',
                borderRadius: 6,
                padding: '8px 10px',
                fontSize: 11,
                color: '#a6adc8',
                fontFamily: 'monospace',
                maxHeight: 200,
                overflowY: 'auto',
                marginBottom: 8,
                whiteSpace: 'pre-wrap',
                wordBreak: 'break-word',
              }}
            >
              {generatedConfig.modules.map((m) => `${m.name} (${m.type})`).join('\n')}
            </div>
            <button
              onClick={handleApply}
              style={{
                width: '100%',
                padding: '8px 14px',
                background: '#10b981',
                border: 'none',
                borderRadius: 6,
                color: '#1e1e2e',
                fontSize: 12,
                fontWeight: 600,
                cursor: 'pointer',
              }}
            >
              Apply to Canvas
            </button>
          </div>
        )}

        {/* Suggest workflows */}
        <div style={{ borderTop: '1px solid #313244', paddingTop: 16 }}>
          <span style={{ color: '#a6adc8', fontSize: 11, display: 'block', marginBottom: 8 }}>
            Explore suggestions
          </span>
          <div style={{ display: 'flex', gap: 6 }}>
            <input
              type="text"
              placeholder="Use case (e.g., e-commerce)"
              style={{
                flex: 1,
                padding: '6px 8px',
                background: '#1e1e2e',
                border: '1px solid #313244',
                borderRadius: 4,
                color: '#cdd6f4',
                fontSize: 12,
                outline: 'none',
              }}
              onKeyDown={(e) => {
                if (e.key === 'Enter') {
                  handleSuggest(e.currentTarget.value);
                }
              }}
            />
            <button
              onClick={() => {
                const input = document.querySelector<HTMLInputElement>('[placeholder*="Use case"]');
                if (input?.value) handleSuggest(input.value);
              }}
              disabled={suggestLoading}
              style={{
                padding: '6px 12px',
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 4,
                color: suggestLoading ? '#585b70' : '#cdd6f4',
                fontSize: 12,
                cursor: suggestLoading ? 'default' : 'pointer',
              }}
            >
              {suggestLoading ? '...' : 'Suggest'}
            </button>
          </div>

          {suggestions.length > 0 && (
            <div style={{ marginTop: 10, display: 'flex', flexDirection: 'column', gap: 6 }}>
              {suggestions.map((s, i) => (
                <div
                  key={i}
                  style={{
                    background: '#1e1e2e',
                    border: '1px solid #313244',
                    borderRadius: 6,
                    padding: '8px 10px',
                    cursor: 'pointer',
                  }}
                  onClick={() => handleQuickGenerate(s.intent)}
                >
                  <div style={{ color: '#cdd6f4', fontSize: 12, fontWeight: 600 }}>{s.title}</div>
                  <div style={{ color: '#a6adc8', fontSize: 11, marginTop: 2 }}>{s.description}</div>
                </div>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
