const { createApp } = Vue;

createApp({
    data() {
        return {
            // Authentication
            isAuthenticated: false,
            token: '',
            user: {},
            tenant: {},
            loginForm: {
                username: '',
                password: ''
            },
            loginError: '',
            
            // UI State
            currentView: 'dashboard',
            isLoading: false,
            
            // Data
            workflows: [],
            executions: [],
            selectedWorkflow: null,
            selectedExecution: null,
            
            // Forms
            workflowForm: {
                name: '',
                description: '',
                config: ''
            },
            editingWorkflow: null,
            
            // Sample YAML template
            sampleYaml: `modules:
  - name: api-http-server
    type: http.server
    config:
      address: ":8080"
  - name: api-router
    type: http.router
  - name: hello-handler
    type: http.handler
    config:
      contentType: application/json

workflows:
  http:
    routes:
      - method: GET
        path: /api/hello
        handler: hello-handler`
        }
    },
    
    computed: {
        completedExecutions() {
            return this.executions.filter(e => e.status === 'completed').length;
        },
        
        failedExecutions() {
            return this.executions.filter(e => e.status === 'failed').length;
        }
    },
    
    mounted() {
        // Check for existing token
        const token = localStorage.getItem('workflow_token');
        if (token) {
            this.token = token;
            this.validateToken();
        }
    },
    
    methods: {
        async validateToken() {
            try {
                const response = await this.apiCall('/api/workflows', 'GET');
                if (response.ok) {
                    this.isAuthenticated = true;
                    await this.loadInitialData();
                } else {
                    localStorage.removeItem('workflow_token');
                    this.token = '';
                }
            } catch (error) {
                localStorage.removeItem('workflow_token');
                this.token = '';
            }
        },
        
        async login() {
            this.isLoading = true;
            this.loginError = '';
            
            try {
                const response = await fetch('/api/login', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify(this.loginForm)
                });
                
                if (response.ok) {
                    const data = await response.json();
                    this.token = data.token;
                    this.user = data.user;
                    this.tenant = data.tenant;
                    this.isAuthenticated = true;
                    
                    localStorage.setItem('workflow_token', this.token);
                    
                    await this.loadInitialData();
                } else {
                    const error = await response.json();
                    this.loginError = error.error || 'Login failed';
                }
            } catch (error) {
                this.loginError = 'Network error occurred';
            } finally {
                this.isLoading = false;
            }
        },
        
        logout() {
            this.isAuthenticated = false;
            this.token = '';
            this.user = {};
            this.tenant = {};
            this.workflows = [];
            this.executions = [];
            localStorage.removeItem('workflow_token');
            this.currentView = 'dashboard';
        },
        
        async loadInitialData() {
            await Promise.all([
                this.loadWorkflows(),
                this.loadAllExecutions()
            ]);
        },
        
        async apiCall(url, method = 'GET', body = null) {
            const headers = {
                'Authorization': `Bearer ${this.token}`,
                'Content-Type': 'application/json'
            };
            
            const config = {
                method,
                headers
            };
            
            if (body) {
                config.body = JSON.stringify(body);
            }
            
            return fetch(url, config);
        },
        
        async loadWorkflows() {
            try {
                const response = await this.apiCall('/api/workflows');
                if (response.ok) {
                    const data = await response.json();
                    this.workflows = data.workflows || [];
                }
            } catch (error) {
                console.error('Failed to load workflows:', error);
            }
        },
        
        async loadAllExecutions() {
            try {
                this.executions = [];
                for (const workflow of this.workflows) {
                    const response = await this.apiCall(`/api/workflows/${workflow.id}/executions`);
                    if (response.ok) {
                        const data = await response.json();
                        this.executions.push(...(data.executions || []));
                    }
                }
                // Sort by date descending
                this.executions.sort((a, b) => new Date(b.started_at) - new Date(a.started_at));
            } catch (error) {
                console.error('Failed to load executions:', error);
            }
        },
        
        setView(view) {
            this.currentView = view;
        },
        
        showCreateWorkflow() {
            this.editingWorkflow = null;
            this.workflowForm = {
                name: '',
                description: '',
                config: this.sampleYaml
            };
            // Try to use Bootstrap modal, fallback to manual show if Bootstrap isn't available
            const modal = document.getElementById('workflowModal');
            if (typeof bootstrap !== 'undefined') {
                new bootstrap.Modal(modal).show();
            } else {
                // Fallback: manually show modal with CSS
                modal.style.display = 'block';
                modal.classList.add('show');
                document.body.classList.add('modal-open');
                // Create backdrop manually
                const backdrop = document.createElement('div');
                backdrop.className = 'modal-backdrop fade show';
                backdrop.id = 'manual-backdrop';
                document.body.appendChild(backdrop);
            }
        },
        
        editWorkflow(workflow) {
            this.editingWorkflow = workflow;
            this.workflowForm = {
                name: workflow.name,
                description: workflow.description,
                config: workflow.config
            };
            // Try to use Bootstrap modal, fallback to manual show if Bootstrap isn't available
            const modal = document.getElementById('workflowModal');
            if (typeof bootstrap !== 'undefined') {
                new bootstrap.Modal(modal).show();
            } else {
                // Fallback: manually show modal with CSS
                modal.style.display = 'block';
                modal.classList.add('show');
                document.body.classList.add('modal-open');
                // Create backdrop manually if it doesn't exist
                if (!document.getElementById('manual-backdrop')) {
                    const backdrop = document.createElement('div');
                    backdrop.className = 'modal-backdrop fade show';
                    backdrop.id = 'manual-backdrop';
                    document.body.appendChild(backdrop);
                }
            }
        },
        
        async saveWorkflow() {
            this.isLoading = true;
            
            try {
                let response;
                if (this.editingWorkflow) {
                    response = await this.apiCall(
                        `/api/workflows/${this.editingWorkflow.id}`,
                        'PUT',
                        this.workflowForm
                    );
                } else {
                    response = await this.apiCall('/api/workflows', 'POST', this.workflowForm);
                }
                
                if (response.ok) {
                    // Close modal with fallback
                    const modal = document.getElementById('workflowModal');
                    if (typeof bootstrap !== 'undefined') {
                        bootstrap.Modal.getInstance(modal).hide();
                    } else {
                        // Fallback: manually hide modal
                        modal.style.display = 'none';
                        modal.classList.remove('show');
                        document.body.classList.remove('modal-open');
                        // Remove manual backdrop
                        const backdrop = document.getElementById('manual-backdrop');
                        if (backdrop) {
                            backdrop.remove();
                        }
                    }
                    await this.loadWorkflows();
                } else {
                    const error = await response.json();
                    alert('Error: ' + (error.error || 'Failed to save workflow'));
                }
            } catch (error) {
                alert('Network error occurred');
            } finally {
                this.isLoading = false;
            }
        },
        
        selectWorkflow(workflow) {
            this.selectedWorkflow = workflow;
            // Could show workflow details in a modal or sidebar
        },
        
        async executeWorkflow(workflow) {
            if (!confirm(`Execute workflow "${workflow.name}"?`)) {
                return;
            }
            
            try {
                const response = await this.apiCall(
                    `/api/workflows/${workflow.id}/execute`,
                    'POST',
                    { input: {} }
                );
                
                if (response.ok) {
                    alert('Workflow execution started');
                    // Refresh executions after a short delay
                    setTimeout(() => this.loadAllExecutions(), 2000);
                } else {
                    const error = await response.json();
                    alert('Error: ' + (error.error || 'Failed to execute workflow'));
                }
            } catch (error) {
                alert('Network error occurred');
            }
        },
        
        viewExecution(execution) {
            this.selectedExecution = execution;
            // Try to use Bootstrap modal, fallback to manual show if Bootstrap isn't available
            const modal = document.getElementById('executionModal');
            if (typeof bootstrap !== 'undefined') {
                new bootstrap.Modal(modal).show();
            } else {
                // Fallback: manually show modal with CSS
                modal.style.display = 'block';
                modal.classList.add('show');
                document.body.classList.add('modal-open');
                // Create backdrop manually if it doesn't exist
                if (!document.getElementById('manual-backdrop')) {
                    const backdrop = document.createElement('div');
                    backdrop.className = 'modal-backdrop fade show';
                    backdrop.id = 'manual-backdrop';
                    document.body.appendChild(backdrop);
                }
            }
        },
        
        getStatusClass(status) {
            switch (status) {
                case 'active':
                case 'running':
                    return 'bg-primary';
                case 'completed':
                    return 'bg-success';
                case 'failed':
                case 'error':
                    return 'bg-danger';
                case 'stopped':
                    return 'bg-secondary';
                default:
                    return 'bg-warning';
            }
        },
        
        formatDate(dateString) {
            const date = new Date(dateString);
            return date.toLocaleString();
        },
        
        getDuration(execution) {
            if (!execution.ended_at) {
                if (execution.status === 'running') {
                    return 'Running...';
                }
                return 'N/A';
            }
            
            const start = new Date(execution.started_at);
            const end = new Date(execution.ended_at);
            const diff = end - start;
            
            const seconds = Math.floor(diff / 1000);
            const minutes = Math.floor(seconds / 60);
            const hours = Math.floor(minutes / 60);
            
            if (hours > 0) {
                return `${hours}h ${minutes % 60}m ${seconds % 60}s`;
            } else if (minutes > 0) {
                return `${minutes}m ${seconds % 60}s`;
            } else {
                return `${seconds}s`;
            }
        },
        
        getWorkflowName(workflowId) {
            const workflow = this.workflows.find(w => w.id === workflowId);
            return workflow ? workflow.name : 'Unknown';
        }
    }
}).mount('#app');