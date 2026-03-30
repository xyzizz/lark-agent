/* feishu-agent 前端公共 JS */

// ─── Toast 提示 ───────────────────────────────────
const Toast = {
  container: null,
  init() {
    if (!this.container) {
      this.container = document.createElement('div');
      this.container.className = 'toast-container';
      document.body.appendChild(this.container);
    }
  },
  show(msg, type = 'info', duration = 3000) {
    this.init();
    const el = document.createElement('div');
    el.className = `toast toast-${type}`;
    el.textContent = msg;
    this.container.appendChild(el);
    setTimeout(() => {
      el.style.opacity = '0';
      el.style.transform = 'translateX(100%)';
      el.style.transition = '0.3s';
      setTimeout(() => el.remove(), 300);
    }, duration);
  },
  success(msg) { this.show(msg, 'success'); },
  error(msg) { this.show(msg, 'error', 5000); },
  info(msg) { this.show(msg, 'info'); },
};

// ─── API 封装 ─────────────────────────────────────
const API = {
  async request(method, url, data) {
    const opts = {
      method,
      headers: { 'Content-Type': 'application/json' },
    };
    if (data) opts.body = JSON.stringify(data);
    try {
      const res = await fetch(url, opts);
      const json = await res.json();
      return json;
    } catch (e) {
      console.error('API error:', e);
      throw e;
    }
  },
  get: (url) => API.request('GET', url),
  post: (url, data) => API.request('POST', url, data),
  put: (url, data) => API.request('PUT', url, data),
  delete: (url) => API.request('DELETE', url),
};

// ─── 模态框 ──────────────────────────────────────
const Modal = {
  open(id) {
    document.getElementById(id)?.classList.add('show');
  },
  close(id) {
    document.getElementById(id)?.classList.remove('show');
  },
  closeAll() {
    document.querySelectorAll('.modal-overlay').forEach(el => el.classList.remove('show'));
  },
};

// 点击遮罩关闭模态框
document.addEventListener('click', e => {
  if (e.target.classList.contains('modal-overlay')) Modal.closeAll();
});

// ─── 状态徽章 ─────────────────────────────────────
function statusBadge(status) {
  const map = {
    success: ['success', '成功'],
    failed: ['error', '失败'],
    running: ['running', '执行中'],
    pending: ['default', '待处理'],
    skipped: ['warning', '跳过'],
  };
  const [cls, label] = map[status] || ['default', status];
  return `<span class="badge badge-${cls}">${label}</span>`;
}

function intentLabel(intent) {
  const map = {
    issue_troubleshooting: '问题排查',
    requirement_writing: '需求编写',
    ignore: '忽略',
    need_more_context: '信息不足',
    risky_action: '高风险操作',
  };
  return map[intent] || intent;
}

// ─── 格式化时间 ───────────────────────────────────
function fmtTime(str) {
  if (!str) return '-';
  const d = new Date(str);
  if (isNaN(d)) return str;
  const pad = n => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// ─── 确认框 ──────────────────────────────────────
function confirm(msg, onOk) {
  if (window.confirm(msg)) onOk();
}

// ─── 高亮当前导航 ─────────────────────────────────
function highlightNav() {
  const path = location.pathname;
  document.querySelectorAll('.nav-item').forEach(el => {
    el.classList.remove('active');
    const href = el.getAttribute('href');
    if (href && (href === path || (path === '/' && href === '/') || (path !== '/' && href !== '/' && path.startsWith(href)))) {
      el.classList.add('active');
    }
  });
}
document.addEventListener('DOMContentLoaded', highlightNav);

// ─── 加载状态 ─────────────────────────────────────
function setLoading(btn, loading) {
  if (!btn) return;
  if (loading) {
    btn._origText = btn.textContent;
    btn.disabled = true;
    btn.textContent = '处理中...';
  } else {
    btn.disabled = false;
    btn.textContent = btn._origText || btn.textContent;
  }
}

// ─── 表单序列化 ───────────────────────────────────
function formData(formId) {
  const form = document.getElementById(formId);
  if (!form) return {};
  const data = {};
  new FormData(form).forEach((v, k) => { data[k] = v; });
  // 处理 checkbox
  form.querySelectorAll('input[type=checkbox]').forEach(el => {
    data[el.name] = el.checked;
  });
  // 处理 number
  form.querySelectorAll('input[type=number]').forEach(el => {
    if (data[el.name] !== undefined) data[el.name] = Number(data[el.name]);
  });
  return data;
}

// ─── 填充表单 ─────────────────────────────────────
function fillForm(formId, data) {
  const form = document.getElementById(formId);
  if (!form || !data) return;
  Object.entries(data).forEach(([k, v]) => {
    const el = form.querySelector(`[name="${k}"]`);
    if (!el) return;
    if (el.type === 'checkbox') el.checked = !!v;
    else el.value = v ?? '';
  });
}

// ─── JSON 数组输入处理 ────────────────────────────
// 将逗号分隔的字符串转为数组，用于 keywords 等字段
function splitTags(str) {
  return str.split(',').map(s => s.trim()).filter(Boolean);
}
function joinTags(arr) {
  return (arr || []).join(', ');
}

// ─── 分页 ────────────────────────────────────────
function renderPagination(containerId, total, page, size, onPageChange) {
  const container = document.getElementById(containerId);
  if (!container) return;
  const totalPages = Math.ceil(total / size);
  let html = `<span class="page-info">共 ${total} 条</span>`;
  if (totalPages <= 1) { container.innerHTML = html; return; }
  html += `<button class="page-btn" onclick="(${onPageChange.toString()})(${page - 1})" ${page <= 1 ? 'disabled' : ''}>上一页</button>`;
  for (let i = 1; i <= totalPages; i++) {
    if (i === page) {
      html += `<button class="page-btn active">${i}</button>`;
    } else if (Math.abs(i - page) <= 2 || i === 1 || i === totalPages) {
      html += `<button class="page-btn" onclick="(${onPageChange.toString()})(${i})">${i}</button>`;
    } else if (Math.abs(i - page) === 3) {
      html += `<span style="padding:0 4px">...</span>`;
    }
  }
  html += `<button class="page-btn" onclick="(${onPageChange.toString()})(${page + 1})" ${page >= totalPages ? 'disabled' : ''}>下一页</button>`;
  container.innerHTML = html;
}
