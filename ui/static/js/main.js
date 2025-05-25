document.addEventListener('DOMContentLoaded', () => {
    // DOM Elements
    const workflowsContainer = document.getElementById('workflows-container');
    const workflowDetail = document.getElementById('workflow-detail');
    const workflowsList = document.getElementById('workflows-list');
    const workflowForm = document.getElementById('workflow-form');
    
    const workflowNameEl = document.getElementById('workflow-name');
    const workflowStatusEl = document.getElementById('workflow-status');
    const workflowConfigEl = document.getElementById('workflow-config');
    
    const formTitle = document.getElementById('form-title');
    const formNameInput = document.getElementById('workflow-form-name');
    const formConfigInput = document.getElementById('workflow-form-config');
    
    const createWorkflowBtn = document.getElementById('create-workflow-btn');
    const editWorkflowBtn = document.getElementById('edit-workflow-btn');
    const deleteWorkflowBtn = document.getElementById('delete-workflow-btn');
    const backBtn = document.getElementById('back-btn');
    const saveWorkflowBtn = document.getElementById('save-workflow-btn');
    const cancelWorkflowBtn = document.getElementById('cancel-workflow-btn');
    
    const modal = document.getElementById('modal');
    const modalTitle = document.getElementById('modal-title');
    const modalMessage = document.getElementById('modal-message');
    const modalConfirm = document.getElementById('modal-confirm');
    const modalCancel = document.getElementById('modal-cancel');
    const closeModal = document.querySelector('.close-modal');
    
    // State
    let currentWorkflow = null;
    let editMode = false;
    
    // Load workflows
    fetchWorkflows();
    
    // Event listeners
    createWorkflowBtn.addEventListener('click', showCreateWorkflowForm);
    editWorkflowBtn.addEventListener('click', showEditWorkflowForm);
    deleteWorkflowBtn.addEventListener('click', confirmDeleteWorkflow);
    backBtn.addEventListener('click', showWorkflowsList);
    saveWorkflowBtn.addEventListener('click', saveWorkflow);
    cancelWorkflowBtn.addEventListener('click', cancelWorkflowForm);
    closeModal.addEventListener('click', closeModalDialog);
    modalCancel.addEventListener('click', closeModalDialog);
    modalConfirm.addEventListener('click', handleModalConfirm);
    
    // Functions
    async function fetchWorkflows() {
        try {
            const response = await fetch('/api/workflows');
            const data = await response.json();
            
            renderWorkflowsList(data.workflows || []);
        } catch (error) {
            console.error('Error fetching workflows:', error);
            workflowsContainer.innerHTML = '<p class="error">Failed to load workflows. Please try again.</p>';
        }
    }
    
    function renderWorkflowsList(workflows) {
        if (workflows.length === 0) {
            workflowsContainer.innerHTML = '<p>No workflows found. Create one to get started.</p>';
            return;
        }
        
        workflowsContainer.innerHTML = '';
        
        workflows.forEach(workflow => {
            const workflowItem = document.createElement('div');
            workflowItem.className = 'workflow-item';
            workflowItem.innerHTML = `
                <span class="name">${workflow.name}</span>
                <span class="status ${workflow.status}">${workflow.status}</span>
                <div class="actions">
                    <button class="btn view-btn">View</button>
                </div>
            `;
            
            const viewBtn = workflowItem.querySelector('.view-btn');
            viewBtn.addEventListener('click', () => viewWorkflowDetails(workflow.name));
            
            workflowsContainer.appendChild(workflowItem);
        });
    }
    
    async function viewWorkflowDetails(workflowName) {
        try {
            const response = await fetch(`/api/workflows/${workflowName}`);
            
            if (!response.ok) {
                throw new Error(`Failed to fetch workflow: ${response.statusText}`);
            }
            
            const workflow = await response.json();
            currentWorkflow = workflow;
            
            workflowNameEl.textContent = workflow.name;
            workflowStatusEl.textContent = workflow.status;
            
            // Format the config as JSON for display
            const config = workflow.config || workflow.data || {};
            workflowConfigEl.textContent = JSON.stringify(config, null, 2);
            
            // Show workflow details and hide other sections
            workflowDetail.classList.remove('hidden');
            workflowsList.classList.add('hidden');
            workflowForm.classList.add('hidden');
        } catch (error) {
            console.error('Error fetching workflow details:', error);
            alert('Failed to load workflow details. Please try again.');
        }
    }
    
    function showWorkflowsList() {
        workflowsList.classList.remove('hidden');
        workflowDetail.classList.add('hidden');
        workflowForm.classList.add('hidden');
        currentWorkflow = null;
    }
    
    function showCreateWorkflowForm() {
        formTitle.textContent = 'Create New Workflow';
        formNameInput.value = '';
        formConfigInput.value = 
`name: "New Workflow"
description: "Workflow configuration"

modules:
  - name: "http-server"
    type: "http.server"
    config:
      address: ":8080"

  - name: "http-router"
    type: "http.router"

workflows:
  http:
    routes:
      - method: GET
        path: /hello
        handler: hello-handler`;
        
        editMode = false;
        workflowForm.classList.remove('hidden');
        workflowsList.classList.add('hidden');
        workflowDetail.classList.add('hidden');
    }
    
    function showEditWorkflowForm() {
        if (!currentWorkflow) return;
        
        formTitle.textContent = 'Edit Workflow';
        formNameInput.value = currentWorkflow.name;
        formConfigInput.value = JSON.stringify(currentWorkflow.config || {}, null, 2);
        
        editMode = true;
        workflowForm.classList.remove('hidden');
        workflowDetail.classList.add('hidden');
    }
    
    function cancelWorkflowForm() {
        if (editMode && currentWorkflow) {
            // Return to detail view if editing
            workflowDetail.classList.remove('hidden');
            workflowForm.classList.add('hidden');
        } else {
            // Return to list if creating new
            showWorkflowsList();
        }
    }
    
    async function saveWorkflow() {
        const name = formNameInput.value.trim();
        let config;
        
        try {
            config = JSON.parse(formConfigInput.value);
        } catch (e) {
            alert('Invalid JSON configuration. Please check your syntax.');
            return;
        }
        
        if (!name) {
            alert('Workflow name is required.');
            return;
        }
        
        try {
            let url = '/api/workflows';
            let method = 'POST';
            
            if (editMode && currentWorkflow) {
                url = `/api/workflows/${currentWorkflow.name}`;
                method = 'PUT';
            }
            
            const response = await fetch(url, {
                method: method,
                headers: {
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify({
                    name: name,
                    config: config
                })
            });
            
            if (!response.ok) {
                throw new Error(`Failed to save workflow: ${response.statusText}`);
            }
            
            const result = await response.json();
            
            // Refresh workflows list
            await fetchWorkflows();
            
            // Show success message and return to list
            alert(`Workflow ${editMode ? 'updated' : 'created'} successfully!`);
            showWorkflowsList();
        } catch (error) {
            console.error('Error saving workflow:', error);
            alert(`Failed to ${editMode ? 'update' : 'create'} workflow. Please try again.`);
        }
    }
    
    function confirmDeleteWorkflow() {
        if (!currentWorkflow) return;
        
        modalTitle.textContent = 'Confirm Delete';
        modalMessage.textContent = `Are you sure you want to delete the workflow "${currentWorkflow.name}"?`;
        modalConfirm.dataset.action = 'delete';
        showModal();
    }
    
    async function deleteWorkflow() {
        if (!currentWorkflow) return;
        
        try {
            const response = await fetch(`/api/workflows/${currentWorkflow.name}`, {
                method: 'DELETE'
            });
            
            if (!response.ok) {
                throw new Error(`Failed to delete workflow: ${response.statusText}`);
            }
            
            await fetchWorkflows();
            showWorkflowsList();
            alert('Workflow deleted successfully!');
        } catch (error) {
            console.error('Error deleting workflow:', error);
            alert('Failed to delete workflow. Please try again.');
        }
    }
    
    function showModal() {
        modal.classList.remove('hidden');
    }
    
    function closeModalDialog() {
        modal.classList.add('hidden');
    }
    
    function handleModalConfirm() {
        const action = modalConfirm.dataset.action;
        
        if (action === 'delete') {
            deleteWorkflow();
        }
        
        closeModalDialog();
    }
});