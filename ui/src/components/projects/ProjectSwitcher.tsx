import { useState, useEffect, useCallback } from 'react';
import {
  apiListCompanies,
  apiListOrgs,
  apiListProjects,
  apiCreateCompany,
  apiCreateOrg,
  apiCreateProject,
  type ApiCompany,
  type ApiProject,
} from '../../utils/api.ts';
import useAuthStore from '../../store/authStore.ts';

interface ProjectSwitcherProps {
  selectedProjectId: string | null;
  onSelectProject: (project: ApiProject) => void;
}

interface OrgNode {
  org: ApiCompany;
  projects: ApiProject[];
  expanded: boolean;
  loading: boolean;
}

interface CompanyNode {
  company: ApiCompany;
  orgs: OrgNode[];
  expanded: boolean;
  loading: boolean;
}

export default function ProjectSwitcher({ selectedProjectId, onSelectProject }: ProjectSwitcherProps) {
  const [companies, setCompanies] = useState<CompanyNode[]>([]);
  const [creating, setCreating] = useState<{ type: 'company' | 'org' | 'project'; parentId?: string } | null>(null);
  const [newName, setNewName] = useState('');
  const userRole = useAuthStore((s) => s.user?.role);

  const loadCompanies = useCallback(async () => {
    try {
      const list = await apiListCompanies();
      setCompanies(
        (list || []).map((c) => ({
          company: c,
          orgs: [],
          expanded: false,
          loading: false,
        })),
      );
    } catch {
      // ignore
    }
  }, []);

  useEffect(() => {
    loadCompanies();
  }, [loadCompanies]);

  const toggleCompany = async (idx: number) => {
    const updated = [...companies];
    const node = updated[idx];
    node.expanded = !node.expanded;

    if (node.expanded && node.orgs.length === 0) {
      node.loading = true;
      setCompanies([...updated]);
      try {
        const orgs = await apiListOrgs(node.company.id);
        node.orgs = (orgs || []).map((o) => ({
          org: o,
          projects: [],
          expanded: false,
          loading: false,
        }));
      } catch {
        // ignore
      }
      node.loading = false;
    }
    setCompanies([...updated]);
  };

  const toggleOrg = async (compIdx: number, orgIdx: number) => {
    const updated = [...companies];
    const orgNode = updated[compIdx].orgs[orgIdx];
    orgNode.expanded = !orgNode.expanded;

    if (orgNode.expanded && orgNode.projects.length === 0) {
      orgNode.loading = true;
      setCompanies([...updated]);
      try {
        const projects = await apiListProjects(orgNode.org.id);
        orgNode.projects = projects || [];
      } catch {
        // ignore
      }
      orgNode.loading = false;
    }
    setCompanies([...updated]);
  };

  const handleCreate = async () => {
    if (!newName.trim() || !creating) return;
    try {
      if (creating.type === 'company') {
        await apiCreateCompany(newName);
        await loadCompanies();
      } else if (creating.type === 'org' && creating.parentId) {
        await apiCreateOrg(creating.parentId, newName);
        // Reload orgs for this company
        const idx = companies.findIndex((c) => c.company.id === creating.parentId);
        if (idx >= 0) {
          const updated = [...companies];
          const orgs = await apiListOrgs(creating.parentId);
          updated[idx].orgs = (orgs || []).map((o) => ({
            org: o,
            projects: [],
            expanded: false,
            loading: false,
          }));
          setCompanies(updated);
        }
      } else if (creating.type === 'project' && creating.parentId) {
        const proj = await apiCreateProject(creating.parentId, newName);
        // Reload projects for this org
        const updated = [...companies];
        for (const comp of updated) {
          const orgNode = comp.orgs.find((o) => o.org.id === creating.parentId);
          if (orgNode) {
            const projects = await apiListProjects(creating.parentId);
            orgNode.projects = projects || [];
            break;
          }
        }
        setCompanies(updated);
        onSelectProject(proj);
      }
    } catch {
      // ignore
    }
    setCreating(null);
    setNewName('');
  };

  return (
    <div
      style={{
        width: 200,
        minWidth: 200,
        background: '#181825',
        borderRight: '1px solid #313244',
        overflowY: 'auto',
        fontSize: 13,
        color: '#cdd6f4',
        display: 'flex',
        flexDirection: 'column',
      }}
    >
      <div
        style={{
          padding: '8px 12px',
          borderBottom: '1px solid #313244',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
        }}
      >
        <span style={{ fontWeight: 600, fontSize: 12, color: '#a6adc8', textTransform: 'uppercase' }}>
          Companies
        </span>
        <button
          onClick={() => setCreating({ type: 'company' })}
          style={addBtnStyle}
          title="New Company"
        >
          +
        </button>
      </div>

      {/* Create inline form */}
      {creating && (
        <div style={{ padding: '6px 8px', borderBottom: '1px solid #313244' }}>
          <div style={{ color: '#a6adc8', fontSize: 11, marginBottom: 4 }}>
            New {creating.type}
          </div>
          <input
            autoFocus
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter') handleCreate();
              if (e.key === 'Escape') { setCreating(null); setNewName(''); }
            }}
            placeholder="Name..."
            style={{
              width: '100%',
              padding: '4px 6px',
              background: '#313244',
              border: '1px solid #45475a',
              borderRadius: 4,
              color: '#cdd6f4',
              fontSize: 12,
              outline: 'none',
              boxSizing: 'border-box',
            }}
          />
          <div style={{ display: 'flex', gap: 4, marginTop: 4 }}>
            <button onClick={handleCreate} style={{ ...addBtnStyle, fontSize: 11, padding: '2px 8px' }}>
              Create
            </button>
            <button
              onClick={() => { setCreating(null); setNewName(''); }}
              style={{ ...addBtnStyle, fontSize: 11, padding: '2px 8px', background: '#45475a' }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      <div style={{ flex: 1, overflowY: 'auto' }}>
        {companies.filter((c) => !c.company.is_system || userRole === 'admin').map((compNode, ci) => (
          <div key={compNode.company.id}>
            <div
              style={{
                padding: '6px 8px',
                display: 'flex',
                alignItems: 'center',
                gap: 4,
                cursor: 'pointer',
                background: compNode.expanded ? '#1e1e2e' : 'transparent',
              }}
              onClick={() => toggleCompany(ci)}
            >
              <span style={{ fontSize: 10, width: 12 }}>{compNode.expanded ? '\u25BC' : '\u25B6'}</span>
              <span style={{
                flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap',
                color: compNode.company.is_system ? '#f9e2af' : undefined,
              }}
              title={compNode.company.is_system ? 'System (Admin Only)' : compNode.company.name}
              >
                {compNode.company.is_system ? '\u{1F512} ' : '\u{1F3E2} '}
                {compNode.company.is_system ? 'System (Admin Only)' : compNode.company.name}
              </span>
              {!compNode.company.is_system && (
                <button
                  onClick={(e) => { e.stopPropagation(); setCreating({ type: 'org', parentId: compNode.company.id }); }}
                  style={{ ...addBtnStyle, fontSize: 10, padding: '0 4px' }}
                  title="New Organization"
                >
                  +
                </button>
              )}
            </div>

            {compNode.expanded && (
              <div style={{ paddingLeft: 12 }}>
                {!compNode.loading && compNode.orgs.length > 0 && (
                  <div style={{ padding: '2px 8px', fontSize: 9, color: '#585b70', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                    Organizations
                  </div>
                )}
                {compNode.loading && (
                  <div style={{ padding: '4px 8px', color: '#6c7086', fontSize: 11 }}>Loading...</div>
                )}
                {compNode.orgs.map((orgNode, oi) => (
                  <div key={orgNode.org.id}>
                    <div
                      style={{
                        padding: '4px 8px',
                        display: 'flex',
                        alignItems: 'center',
                        gap: 4,
                        cursor: 'pointer',
                        background: orgNode.expanded ? '#1e1e2e' : 'transparent',
                      }}
                      onClick={() => toggleOrg(ci, oi)}
                    >
                      <span style={{ fontSize: 10, width: 12 }}>{orgNode.expanded ? '\u25BC' : '\u25B6'}</span>
                      <span
                        style={{ flex: 1, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}
                        title={orgNode.org.name}
                      >
                        {orgNode.org.name}
                      </span>
                      {!compNode.company.is_system && (
                        <button
                          onClick={(e) => {
                            e.stopPropagation();
                            setCreating({ type: 'project', parentId: orgNode.org.id });
                          }}
                          style={{ ...addBtnStyle, fontSize: 10, padding: '0 4px' }}
                          title="New Project"
                        >
                          +
                        </button>
                      )}
                    </div>

                    {orgNode.expanded && (
                      <div style={{ paddingLeft: 12 }}>
                        {!orgNode.loading && orgNode.projects.length > 0 && (
                          <div style={{ padding: '2px 8px', fontSize: 9, color: '#585b70', textTransform: 'uppercase', letterSpacing: '0.05em' }}>
                            Projects
                          </div>
                        )}
                        {orgNode.loading && (
                          <div style={{ padding: '2px 8px', color: '#6c7086', fontSize: 11 }}>Loading...</div>
                        )}
                        {orgNode.projects.map((proj) => (
                          <div
                            key={proj.id}
                            onClick={() => onSelectProject(proj)}
                            title={proj.name}
                            style={{
                              padding: '4px 8px',
                              cursor: 'pointer',
                              borderRadius: 4,
                              background: selectedProjectId === proj.id ? 'rgba(137, 180, 250, 0.15)' : 'transparent',
                              color: selectedProjectId === proj.id ? '#89b4fa' : '#cdd6f4',
                              overflow: 'hidden',
                              textOverflow: 'ellipsis',
                              whiteSpace: 'nowrap',
                            }}
                          >
                            {proj.name}
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>
            )}
          </div>
        ))}

        {companies.length === 0 && (
          <div style={{ padding: '16px 12px', color: '#6c7086', fontSize: 12, textAlign: 'center' }}>
            No companies yet.
            <br />
            Click + to create one.
          </div>
        )}
      </div>
    </div>
  );
}

const addBtnStyle: React.CSSProperties = {
  background: '#313244',
  border: 'none',
  borderRadius: 4,
  color: '#89b4fa',
  cursor: 'pointer',
  fontSize: 14,
  fontWeight: 700,
  padding: '0 6px',
  lineHeight: '18px',
};
