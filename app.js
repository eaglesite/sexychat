const chatMessages = document.getElementById('chatMessages');
const messageInput = document.getElementById('messageInput');
const sendButton = document.getElementById('sendButton');
const roleGrid = document.getElementById('roleGrid');
const roleInfo = document.getElementById('roleInfo');
const roleSelector = document.getElementById('roleSelector');
const customRoleModal = document.getElementById('customRoleModal');
const customRoleName = document.getElementById('customRoleName');
const customRoleDesc = document.getElementById('customRoleDesc');
const customSystemPrompt = document.getElementById('customSystemPrompt');
const createCustomRoleBtn = document.getElementById('createCustomRoleBtn');
const toggleBtn = document.getElementById('toggleBtn');
const bgSwitcher = document.getElementById('bgSwitcher');
const resetBtn = document.getElementById('resetBtn');

let sessionID = '';
let roles = [];
let selectedRole = null;
let isRoleSelectorCollapsed = false;
let currentBgIndex = 0;
let backgroundImages = [];

window.changeBgNow = function() {
    changeBackground();
};

async function loadBackgrounds() {
    try {
        const response = await fetch('/api/backgrounds');
        if (response.ok) {
            const data = await response.json();
            backgroundImages = data.images || [];
            console.log('Loaded background images:', backgroundImages.length);
        } else {
            backgroundImages = ['bg/2oxy1s.jpg'];
        }
    } catch (error) {
        backgroundImages = ['bg/2oxy1s.jpg'];
    }
}

function changeBackground() {
    const el = document.getElementById('chatMessages');
    if (!el || backgroundImages.length === 0) return;
    currentBgIndex = (currentBgIndex + 1) % backgroundImages.length;
    const image = backgroundImages[currentBgIndex];
    el.style.setProperty('--bg-image', `url('${image}')`);
    localStorage.setItem('chatBackground', image);
}

function loadSavedBackground() {
    const saved = localStorage.getItem('chatBackground');
    const el = document.getElementById('chatMessages');
    if (!el) return;
    if (saved) {
        el.style.setProperty('--bg-image', `url('${saved}')`);
        const index = backgroundImages.indexOf(saved);
        if (index !== -1) currentBgIndex = index;
    }
}

async function toggleRoleSelector() {
    isRoleSelectorCollapsed = !isRoleSelectorCollapsed;
    if (isRoleSelectorCollapsed) {
        roleSelector.classList.add('collapsed');
        toggleBtn.classList.add('collapsed');
    } else {
        roleSelector.classList.remove('collapsed');
        toggleBtn.classList.remove('collapsed');
    }
}

function collapseRoleSelector() {
    isRoleSelectorCollapsed = true;
    roleSelector.classList.add('collapsed');
    toggleBtn.classList.add('collapsed');
}

async function resetSession() {
    if (!selectedRole) return;
    
    if (sessionID) {
        try {
            await fetch('/api/session/delete', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({ session_id: sessionID }),
            });
        } catch (error) {
            console.log('Failed to delete session:', error);
        }
    }
    
    chatMessages.innerHTML = '';
    sessionID = '';
    resetBtn.style.display = 'none';
    
    selectedRole = null;
    messageInput.disabled = true;
    messageInput.placeholder = '请先选择角色...';
    sendButton.disabled = true;
    roleInfo.textContent = '选择一个角色开始';
    
    roleSelector.classList.remove('collapsed');
    toggleBtn.classList.remove('collapsed');
    chatMessages.classList.remove('show');
    
    // 清除保存的角色，防止 autoLoadSavedRole 自动恢复
    localStorage.removeItem('selectedRole');
    loadRoles();
}

function saveSelectedRole(role) {
    try {
        localStorage.setItem('selectedRole', JSON.stringify(role));
    } catch (e) {
        console.error('Failed to save role:', e);
    }
}

function loadSavedRole() {
    try {
        const saved = localStorage.getItem('selectedRole');
        return saved ? JSON.parse(saved) : null;
    } catch (e) {
        return null;
    }
}

function getTime() {
    const now = new Date();
    return now.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
}

function addMessage(text, type) {
    const messageDiv = document.createElement('div');
    messageDiv.className = `message ${type}`;
    messageDiv.innerHTML = `<p>${text}</p><div class="message-time">${getTime()}</div>`;
    chatMessages.appendChild(messageDiv);
    chatMessages.scrollTop = chatMessages.scrollHeight;
}

function addErrorMessage(text) {
    const errorDiv = document.createElement('div');
    errorDiv.className = 'error-message';
    errorDiv.textContent = text;
    chatMessages.appendChild(errorDiv);
    chatMessages.scrollTop = chatMessages.scrollHeight;
}

function addTypingIndicator() {
    const typingDiv = document.createElement('div');
    typingDiv.className = 'typing-indicator';
    typingDiv.innerHTML = '<div class="typing-dots"><span></span><span></span><span></span></div>';
    chatMessages.appendChild(typingDiv);
    chatMessages.scrollTop = chatMessages.scrollHeight;
    return typingDiv;
}

function getCustomRoles() {
    const saved = localStorage.getItem('customRoles');
    if (saved) {
        try {
            return JSON.parse(saved);
        } catch {
            return [];
        }
    }
    return [];
}

function saveCustomRole(role) {
    const customRoles = getCustomRoles();
    role.id = 'custom_' + Date.now();
    role.isCustom = true;
    customRoles.push(role);
    localStorage.setItem('customRoles', JSON.stringify(customRoles));
}

async function loadRoles() {
    try {
        const response = await fetch('/api/roles');
        if (!response.ok) throw new Error('加载角色失败');
        const data = await response.json();
        const customRoles = getCustomRoles();
        roles = [...data.roles, ...customRoles];
        renderRoles();
    } catch (error) {
        roleGrid.innerHTML = '<div style="color: #ec4899;">加载角色失败</div>';
    }
}

function deleteCustomRole(roleId) {
    const customRoles = getCustomRoles();
    const filtered = customRoles.filter(r => r.id !== roleId);
    localStorage.setItem('customRoles', JSON.stringify(filtered));
    loadRoles();
}

function renderRoles(skipAutoLoad = false) {
    roleGrid.innerHTML = '';
    roles.forEach(role => {
        const card = document.createElement('div');
        card.className = role.isCustom ? 'role-card custom-role' : 'role-card';
        card.dataset.roleId = role.id;
        
        let cardContent = `<div class="role-name">${role.name}`;
        if (role.isCustom) {
            cardContent += '<span class="custom-badge">自定义</span>';
        }
        cardContent += `</div><div class="role-desc">${role.description}</div>`;
        
        if (role.isCustom) {
            cardContent += '<button class="delete-role-btn" data-role-id="' + role.id + '">×</button>';
        }
        
        card.innerHTML = cardContent;
        card.addEventListener('click', () => selectRole(role));
        roleGrid.appendChild(card);
    });

    // 绑定删除按钮事件（避免内联onclick）
    roleGrid.querySelectorAll('.delete-role-btn').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            deleteCustomRole(btn.dataset.roleId);
        });
    });

    const customCard = document.createElement('div');
    customCard.className = 'role-card add-custom';
    customCard.innerHTML = '<div class="role-name">+ 新建角色</div><div class="role-desc">创建专属角色</div>';
    customCard.addEventListener('click', openCustomRoleModal);
    roleGrid.appendChild(customCard);

    if (!skipAutoLoad) {
        const savedRole = loadSavedRole();
        if (savedRole) {
            setTimeout(() => autoLoadSavedRole(savedRole), 100);
        }
    }
}

async function autoLoadSavedRole(savedRole) {
    if (savedRole.id && savedRole.id.startsWith('custom')) {
        selectedRole = savedRole;
        collapseRoleSelector();
        const customCard = document.querySelector('.role-card.custom-role');
        if (customCard) customCard.classList.add('selected');
        roleInfo.textContent = `当前: ${savedRole.name}`;
        messageInput.placeholder = '输入消息...';
        messageInput.disabled = false;
        sendButton.disabled = false;
        chatMessages.innerHTML = '';
        chatMessages.classList.add('show');
        resetBtn.style.display = 'flex';

        try {
            const response = await fetch('/api/session', {
                method: 'POST',
                headers: { 'Content-Type': 'application/json' },
                body: JSON.stringify({
                    custom_role_name: savedRole.name,
                    custom_role_description: savedRole.description,
                    custom_system_prompt: savedRole.systemPrompt || savedRole.system_prompt || ''
                }),
            });
            if (response.ok) {
                const data = await response.json();
                sessionID = data.session_id;
                addMessage(data.role.name + ': 你好！我是' + data.role.name + '，有什么可以帮助你的吗？', 'received');
            }
        } catch (error) {
            addErrorMessage('加载失败，请重新选择');
        }
    } else {
        const role = roles.find(r => r.id === savedRole.id);
        if (role) selectRole(role);
    }
}

function openCustomRoleModal() {
    customRoleName.value = '';
    customRoleDesc.value = '';
    customSystemPrompt.value = '';
    customRoleModal.classList.add('show');
    updateCreateButton();
}

function closeCustomRoleModal() {
    customRoleModal.classList.remove('show');
}

function updateCreateButton() {
    const hasName = customRoleName.value.trim() !== '';
    const hasPrompt = customSystemPrompt.value.trim() !== '';
    createCustomRoleBtn.disabled = !(hasName && hasPrompt);
}

customRoleName.addEventListener('input', updateCreateButton);
customSystemPrompt.addEventListener('input', updateCreateButton);
createCustomRoleBtn.addEventListener('click', createCustomRole);

async function createCustomRole() {
    const name = customRoleName.value.trim();
    const desc = customRoleDesc.value.trim();
    const prompt = customSystemPrompt.value.trim();
    if (!name || !prompt) return;

    closeCustomRoleModal();
    const customRole = { name, description: desc || '自定义角色', systemPrompt: prompt };
    saveCustomRole(customRole);
    const savedRoles = getCustomRoles();
    const actualRole = savedRoles[savedRoles.length - 1];
    
    selectedRole = actualRole;
    saveSelectedRole(actualRole);
    
    const resp = await fetch('/api/roles');
    if (resp.ok) {
        const data = await resp.json();
        roles = [...data.roles, ...getCustomRoles()];
        renderRoles(true);
    }
    selectRole(actualRole);
}

function selectRole(role) {
    selectedRole = role;
    saveSelectedRole(role);
    collapseRoleSelector();
    document.querySelectorAll('.role-card').forEach(card => card.classList.remove('selected'));
    const target = document.querySelector(`[data-role-id="${role.id}"]`);
    if (target) target.classList.add('selected');
    roleInfo.textContent = `当前: ${role.name}`;
    messageInput.placeholder = '输入消息...';
    messageInput.disabled = false;
    sendButton.disabled = false;
    chatMessages.innerHTML = '';
    chatMessages.classList.add('show');
    resetBtn.style.display = 'flex';
    
    if (role.isCustom) {
        createCustomSession(role);
    } else {
        createSession(role.id);
    }
}

async function createCustomSession(role) {
    try {
        const response = await fetch('/api/session', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                custom_role_name: role.name,
                custom_role_description: role.description,
                custom_system_prompt: role.systemPrompt || role.system_prompt || ''
            }),
        });
        if (response.ok) {
            const data = await response.json();
            sessionID = data.session_id;
            addMessage(data.role.name + ': 你好！我是' + data.role.name + '，有什么可以帮助你的吗？', 'received');
        }
    } catch (error) {
        addErrorMessage('创建会话失败');
    }
}

async function createSession(roleId) {
    try {
        const response = await fetch('/api/session', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ role_id: roleId }),
        });
        if (response.ok) {
            const data = await response.json();
            sessionID = data.session_id;
            addMessage(data.role.name + ': 你好！我是' + data.role.name + '，有什么可以帮助你的吗？', 'received');
        }
    } catch (error) {
        addErrorMessage('创建会话失败');
    }
}

async function sendMessage() {
    const text = messageInput.value.trim();
    if (!text || !sessionID) return;

    addMessage(text, 'sent');
    messageInput.value = '';
    sendButton.disabled = true;
    const typingIndicator = addTypingIndicator();

    try {
        const response = await fetch('/api/chat', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ session_id: sessionID, message: text }),
        });
        typingIndicator.remove();

        if (!response.ok) {
            if (response.status === 404) {
                addErrorMessage('会话已过期，请重新选择角色');
                sessionID = '';
                messageInput.disabled = true;
                sendButton.disabled = true;
            }
            return;
        }
        const data = await response.json();
        addMessage(selectedRole.name + ': ' + data.message, 'received');
    } catch (error) {
        typingIndicator.remove();
        addErrorMessage('发送失败，请检查后端服务');
    } finally {
        sendButton.disabled = false;
    }
}

// ===== 初始化 =====
document.getElementById('roleSelectorHeader').addEventListener('click', toggleRoleSelector);
document.getElementById('btnCancelModal').addEventListener('click', closeCustomRoleModal);
resetBtn.addEventListener('click', resetSession);

loadRoles();
loadBackgrounds().then(() => {
    loadSavedBackground();
});

// 每10分钟自动刷新角色列表
setInterval(async () => {
    try {
        const resp = await fetch('/api/roles');
        if (resp.ok) {
            const data = await resp.json();
            roles = [...data.roles, ...getCustomRoles()];
            renderRoles(true);
        }
    } catch (e) {}
}, 600000);

sendButton.addEventListener('click', sendMessage);
messageInput.addEventListener('keypress', (e) => {
    if (e.key === 'Enter' && !sendButton.disabled) sendMessage();
});

if (bgSwitcher) {
    bgSwitcher.addEventListener('click', changeBackground);
}

document.addEventListener('click', (e) => {
    if (e.target === customRoleModal) closeCustomRoleModal();
});

// 手机键盘适配
if (window.visualViewport) {
    window.visualViewport.addEventListener('resize', () => {
        const msgs = document.getElementById('chatMessages');
        if (msgs && msgs.classList.contains('show')) {
            requestAnimationFrame(() => {
                msgs.scrollTop = msgs.scrollHeight;
            });
        }
    });
}
