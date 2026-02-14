import { useEffect, useState, useCallback } from 'react';
import {
  apiListUsers,
  apiCreateUser,
  apiDeleteUser,
  apiUpdateUserRole,
  type AdminUser,
} from '../../utils/api.ts';
import useAuthStore from '../../store/authStore.ts';

function AddUserModal({ onClose, onCreated }: { onClose: () => void; onCreated: () => void }) {
  const [email, setEmail] = useState('');
  const [name, setName] = useState('');
  const [password, setPassword] = useState('');
  const [role, setRole] = useState('user');
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    if (password.length < 6) {
      setError('Password must be at least 6 characters');
      return;
    }
    setSaving(true);
    try {
      await apiCreateUser(email, password, name, role);
      onCreated();
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to create user');
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      style={{
        position: 'fixed',
        inset: 0,
        background: 'rgba(0,0,0,0.6)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        zIndex: 1000,
      }}
      onClick={onClose}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        style={{
          background: '#1e1e2e',
          border: '1px solid #45475a',
          borderRadius: 12,
          padding: 24,
          width: 400,
        }}
      >
        <h3 style={{ color: '#cdd6f4', margin: '0 0 16px', fontSize: 16 }}>Add User</h3>

        {error && (
          <div
            style={{
              background: 'rgba(243, 139, 168, 0.15)',
              border: '1px solid #f38ba8',
              borderRadius: 6,
              padding: '8px 12px',
              marginBottom: 12,
              color: '#f38ba8',
              fontSize: 13,
            }}
          >
            {error}
          </div>
        )}

        <form onSubmit={handleSubmit}>
          <div style={{ marginBottom: 12 }}>
            <label style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>Name</label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="User name"
              style={inputStyle}
            />
          </div>
          <div style={{ marginBottom: 12 }}>
            <label style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>Email</label>
            <input
              type="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              required
              placeholder="user@example.com"
              style={inputStyle}
            />
          </div>
          <div style={{ marginBottom: 12 }}>
            <label style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>Password</label>
            <input
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              placeholder="Min 6 characters"
              style={inputStyle}
            />
          </div>
          <div style={{ marginBottom: 12 }}>
            <label style={{ color: '#a6adc8', fontSize: 12, display: 'block', marginBottom: 4 }}>Role</label>
            <select
              value={role}
              onChange={(e) => setRole(e.target.value)}
              style={{
                width: '100%',
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 6,
                color: '#cdd6f4',
                padding: '8px 12px',
                fontSize: 13,
                outline: 'none',
              }}
            >
              <option value="user">User</option>
              <option value="admin">Admin</option>
            </select>
          </div>

          <div style={{ display: 'flex', gap: 8, marginTop: 20 }}>
            <button
              type="submit"
              disabled={saving}
              style={{
                background: saving ? '#585b70' : '#89b4fa',
                border: 'none',
                borderRadius: 6,
                color: '#1e1e2e',
                padding: '8px 20px',
                fontSize: 13,
                fontWeight: 600,
                cursor: saving ? 'not-allowed' : 'pointer',
              }}
            >
              {saving ? 'Creating...' : 'Create'}
            </button>
            <button
              type="button"
              onClick={onClose}
              style={{
                background: '#313244',
                border: '1px solid #45475a',
                borderRadius: 6,
                color: '#cdd6f4',
                padding: '8px 20px',
                fontSize: 13,
                cursor: 'pointer',
              }}
            >
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  width: '100%',
  background: '#313244',
  border: '1px solid #45475a',
  borderRadius: 6,
  color: '#cdd6f4',
  padding: '8px 12px',
  fontSize: 13,
  outline: 'none',
  boxSizing: 'border-box',
};

export default function UserManagement() {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [showAddModal, setShowAddModal] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);
  const currentUser = useAuthStore((s) => s.user);

  const loadUsers = useCallback(async () => {
    try {
      const data = await apiListUsers();
      setUsers(data);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load users');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadUsers();
  }, [loadUsers]);

  const handleRoleChange = async (userId: string, newRole: string) => {
    try {
      await apiUpdateUserRole(userId, newRole);
      await loadUsers();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update role');
    }
  };

  const handleDelete = async (userId: string) => {
    try {
      await apiDeleteUser(userId);
      setConfirmDelete(null);
      await loadUsers();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete user');
    }
  };

  return (
    <div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
        <h3 style={{ color: '#cdd6f4', margin: 0, fontSize: 16, fontWeight: 600 }}>Users</h3>
        <span style={{ flex: 1 }} />
        <button
          onClick={() => setShowAddModal(true)}
          style={{
            background: '#89b4fa',
            border: 'none',
            borderRadius: 6,
            color: '#1e1e2e',
            padding: '8px 16px',
            fontSize: 13,
            fontWeight: 600,
            cursor: 'pointer',
          }}
        >
          Add User
        </button>
      </div>

      {error && (
        <div
          style={{
            background: 'rgba(243, 139, 168, 0.15)',
            border: '1px solid #f38ba8',
            borderRadius: 6,
            padding: '8px 12px',
            marginBottom: 12,
            color: '#f38ba8',
            fontSize: 13,
          }}
        >
          {error}
        </div>
      )}

      {loading ? (
        <div style={{ color: '#6c7086', fontSize: 13, padding: 20, textAlign: 'center' }}>
          Loading users...
        </div>
      ) : (
        <div style={{ background: '#313244', borderRadius: 8, overflow: 'hidden' }}>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '2fr 2fr 100px 140px 80px',
              padding: '10px 16px',
              background: '#181825',
              fontSize: 11,
              color: '#a6adc8',
              fontWeight: 600,
            }}
          >
            <span>Email</span>
            <span>Name</span>
            <span>Role</span>
            <span>Created</span>
            <span>Actions</span>
          </div>
          {users.map((user, i) => (
            <div key={user.id}>
              <div
                style={{
                  display: 'grid',
                  gridTemplateColumns: '2fr 2fr 100px 140px 80px',
                  padding: '10px 16px',
                  borderBottom: '1px solid #45475a',
                  fontSize: 13,
                  background: i % 2 === 0 ? 'transparent' : '#181825',
                  alignItems: 'center',
                }}
              >
                <span style={{ color: '#cdd6f4', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                  {user.email}
                </span>
                <span style={{ color: '#a6adc8' }}>{user.name || '-'}</span>
                <span>
                  <select
                    value={user.role || 'user'}
                    onChange={(e) => handleRoleChange(user.id, e.target.value)}
                    style={{
                      background: '#313244',
                      border: '1px solid #45475a',
                      borderRadius: 4,
                      color: '#cdd6f4',
                      padding: '2px 6px',
                      fontSize: 11,
                      outline: 'none',
                    }}
                  >
                    <option value="user">user</option>
                    <option value="admin">admin</option>
                  </select>
                </span>
                <span style={{ color: '#6c7086', fontSize: 11 }}>
                  {user.createdAt ? new Date(user.createdAt).toLocaleDateString() : '-'}
                </span>
                <span>
                  {currentUser?.id !== user.id && (
                    <button
                      onClick={() => setConfirmDelete(user.id)}
                      style={{
                        background: '#f38ba822',
                        border: '1px solid #f38ba8',
                        borderRadius: 4,
                        color: '#f38ba8',
                        fontSize: 10,
                        padding: '2px 8px',
                        cursor: 'pointer',
                      }}
                    >
                      Delete
                    </button>
                  )}
                </span>
              </div>

              {confirmDelete === user.id && (
                <div
                  style={{
                    padding: '8px 16px',
                    background: '#f38ba811',
                    borderBottom: '1px solid #45475a',
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    fontSize: 12,
                    color: '#f38ba8',
                  }}
                >
                  <span>Delete {user.email}?</span>
                  <button
                    onClick={() => handleDelete(user.id)}
                    style={{
                      background: '#f38ba8',
                      border: 'none',
                      borderRadius: 4,
                      color: '#1e1e2e',
                      fontSize: 11,
                      fontWeight: 600,
                      padding: '3px 10px',
                      cursor: 'pointer',
                    }}
                  >
                    Confirm
                  </button>
                  <button
                    onClick={() => setConfirmDelete(null)}
                    style={{
                      background: '#313244',
                      border: '1px solid #45475a',
                      borderRadius: 4,
                      color: '#cdd6f4',
                      fontSize: 11,
                      padding: '3px 10px',
                      cursor: 'pointer',
                    }}
                  >
                    Cancel
                  </button>
                </div>
              )}
            </div>
          ))}
          {users.length === 0 && (
            <div style={{ padding: 20, color: '#6c7086', fontSize: 13, textAlign: 'center' }}>
              No users found.
            </div>
          )}
        </div>
      )}

      {showAddModal && (
        <AddUserModal onClose={() => setShowAddModal(false)} onCreated={loadUsers} />
      )}
    </div>
  );
}
