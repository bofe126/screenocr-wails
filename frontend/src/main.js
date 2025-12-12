// Wails 运行时绑定
const { GetConfig, SaveConfig, Translate, HideWindow } = window.go?.main?.App || {};

// DOM 元素
const elements = {
    delaySlider: document.getElementById('delaySlider'),
    delayValue: document.getElementById('delayValue'),
    hotkeyBtn: document.getElementById('hotkeyBtn'),
    ocrEngineRadios: document.querySelectorAll('input[name="ocrEngine"]'),
    enableTranslation: document.getElementById('enableTranslation'),
    targetLang: document.getElementById('targetLang'),
    secretId: document.getElementById('secretId'),
    secretKey: document.getElementById('secretKey'),
    autoCopy: document.getElementById('autoCopy'),
    imagePreprocess: document.getElementById('imagePreprocess'),
    showDebug: document.getElementById('showDebug'),
    saveBtn: document.getElementById('saveBtn'),
    hideBtn: document.getElementById('hideBtn'),
    apiLink: document.getElementById('apiLink'),
};

// 当前配置
let currentConfig = {};

function setTranslationUIEnabled(enabled) {
    const section = document.getElementById('translationSection');
    const apiSection = document.getElementById('apiSection');

    if (section) {
        section.classList.toggle('is-disabled', !enabled);
    }

    const toToggle = [
        elements.targetLang,
        elements.secretId,
        elements.secretKey,
        apiSection,
    ];

    toToggle.forEach((el) => {
        if (!el) return;

        if (el instanceof HTMLElement && el.tagName === 'DIV') {
            el.classList.toggle('is-disabled', !enabled);
            return;
        }

        if ('disabled' in el) {
            el.disabled = !enabled;
        }
    });
}

// 初始化
async function init() {
    // 加载配置
    await loadConfig();

    // 绑定事件
    bindEvents();
}

// 加载配置
async function loadConfig() {
    try {
        if (GetConfig) {
            currentConfig = await GetConfig();
            applyConfigToUI(currentConfig);
        }
    } catch (err) {
        console.error('加载配置失败:', err);
    }
}

// 应用配置到 UI
function applyConfigToUI(config) {
    // 延时滑块
    elements.delaySlider.value = config.trigger_delay_ms || 300;
    elements.delayValue.textContent = `${elements.delaySlider.value} ms`;

    // 快捷键
    elements.hotkeyBtn.textContent = (config.hotkey || 'ALT').toUpperCase();

    // OCR 引擎
    elements.ocrEngineRadios.forEach(radio => {
        radio.checked = radio.value === (config.ocr_engine || 'windows');
    });

    // 翻译设置
    elements.enableTranslation.checked = config.enable_translation !== false;
    elements.targetLang.value = config.translation_target || 'zh';
    elements.secretId.value = config.tencent_secret_id || '';
    elements.secretKey.value = config.tencent_secret_key || '';

    setTranslationUIEnabled(elements.enableTranslation.checked);

    // 其他选项
    elements.autoCopy.checked = config.auto_copy !== false;
    elements.imagePreprocess.checked = config.image_preprocess === true;
    elements.showDebug.checked = config.show_debug === true;
}

// 从 UI 获取配置
function getConfigFromUI() {
    let selectedEngine = 'windows';
    elements.ocrEngineRadios.forEach(radio => {
        if (radio.checked) selectedEngine = radio.value;
    });

    return {
        trigger_delay_ms: parseInt(elements.delaySlider.value),
        hotkey: elements.hotkeyBtn.textContent.toLowerCase(),
        ocr_engine: selectedEngine,
        enable_translation: elements.enableTranslation.checked,
        translation_source: 'auto',
        translation_target: elements.targetLang.value,
        tencent_secret_id: elements.secretId.value,
        tencent_secret_key: elements.secretKey.value,
        auto_copy: elements.autoCopy.checked,
        image_preprocess: elements.imagePreprocess.checked,
        show_debug: elements.showDebug.checked,
        // 保留原有配置值，避免覆盖用户设置
        first_run: currentConfig.first_run !== undefined ? currentConfig.first_run : false,
        show_welcome: currentConfig.show_welcome !== undefined ? currentConfig.show_welcome : false,
        show_startup_notification: currentConfig.show_startup_notification !== undefined ? currentConfig.show_startup_notification : true,
    };
}

// 绑定事件
function bindEvents() {
    // 延时滑块
    elements.delaySlider.addEventListener('input', () => {
        const value = Math.round(elements.delaySlider.value / 50) * 50;
        elements.delaySlider.value = value;
        elements.delayValue.textContent = `${value} ms`;
    });

    // 快捷键按钮
    let recording = false;
    let pressedKeys = new Set();

    elements.hotkeyBtn.addEventListener('click', () => {
        if (!recording) {
            recording = true;
            elements.hotkeyBtn.textContent = '按下快捷键...';
            elements.hotkeyBtn.classList.add('recording');
            pressedKeys.clear();
        }
    });

    document.addEventListener('keydown', (e) => {
        if (!recording) return;
        e.preventDefault();

        const keyName = getKeyName(e);
        if (keyName) {
            pressedKeys.add(keyName);
            elements.hotkeyBtn.textContent = Array.from(pressedKeys).join('+');
        }
    });

    document.addEventListener('keyup', (e) => {
        if (!recording) return;
        
        const keyName = getKeyName(e);
        if (keyName && pressedKeys.has(keyName)) {
            pressedKeys.delete(keyName);
        }

        if (pressedKeys.size === 0) {
            recording = false;
            elements.hotkeyBtn.classList.remove('recording');
            // 保持当前显示的快捷键
        }
    });

    // 保存按钮
    elements.saveBtn.addEventListener('click', async () => {
        try {
            const config = getConfigFromUI();
            if (SaveConfig) {
                await SaveConfig(config);
                showToast('设置已保存');
            }
        } catch (err) {
            console.error('保存配置失败:', err);
            showToast('保存失败: ' + err.message, 'error');
        }
    });

    // 隐藏按钮
    elements.hideBtn.addEventListener('click', () => {
        if (HideWindow) {
            HideWindow();
        }
    });

    // API 链接
    elements.apiLink.addEventListener('click', (e) => {
        e.preventDefault();
        if (window.runtime?.BrowserOpenURL) {
            window.runtime.BrowserOpenURL('https://console.cloud.tencent.com/cam/capi');
        }
    });
}

// 获取键名
function getKeyName(e) {
    const keyMap = {
        'Control': 'CTRL',
        'Alt': 'ALT',
        'Shift': 'SHIFT',
        'Meta': 'WIN',
    };

    if (keyMap[e.key]) {
        return keyMap[e.key];
    }

    if (e.key.length === 1) {
        return e.key.toUpperCase();
    }

    if (e.key.startsWith('F') && e.key.length <= 3) {
        return e.key.toUpperCase();
    }

    return null;
}

// 显示提示
function showToast(message, type = 'success') {
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    toast.style.cssText = `
        position: fixed;
        bottom: 80px;
        left: 50%;
        transform: translateX(-50%);
        padding: 12px 24px;
        background: ${type === 'success' ? '#4ecca3' : '#e94560'};
        color: white;
        border-radius: 8px;
        font-size: 14px;
        z-index: 1000;
        animation: fadeInUp 0.3s ease;
    `;
    
    document.body.appendChild(toast);
    
    setTimeout(() => {
        toast.style.animation = 'fadeOutDown 0.3s ease';
        setTimeout(() => toast.remove(), 300);
    }, 2000);
}

// 添加动画样式
const style = document.createElement('style');
style.textContent = `
    @keyframes fadeInUp {
        from { opacity: 0; transform: translateX(-50%) translateY(20px); }
        to { opacity: 1; transform: translateX(-50%) translateY(0); }
    }
    @keyframes fadeOutDown {
        from { opacity: 1; transform: translateX(-50%) translateY(0); }
        to { opacity: 0; transform: translateX(-50%) translateY(20px); }
    }
`;
document.head.appendChild(style);

// 启动
init();

