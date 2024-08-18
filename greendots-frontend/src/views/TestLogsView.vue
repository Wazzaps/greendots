<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

const iframe = ref<HTMLIFrameElement | null>(null);
const router = useRouter();
const route = useRoute();

const labels = {
  d: 'Debug',
  i: 'Info',
  w: 'Warn',
  e: 'Error',
  c: 'Critical',
  t: 'Timestamps'
};

let hidden_log_levels: string[] = JSON.parse(localStorage.getItem('hidden_log_levels') || '[]');

function toggleStatus(e: Event, level: string) {
  if (!iframe.value) {
    return;
  }
  const checked = (e.target! as HTMLInputElement).checked;
  const className = 'hide-' + level;
  const body = iframe.value.contentWindow?.document.body;
  if (body) {
    if (checked) {
      body.classList.remove(className);
      hidden_log_levels = hidden_log_levels.filter((l) => l !== level);
    } else {
      body.classList.add(className);
      hidden_log_levels.push(level);
    }
  }
  localStorage.setItem('hidden_log_levels', JSON.stringify(hidden_log_levels));
}

let stylesheet: CSSStyleSheet | null = null;
let rule_map: { [logger: string]: number } = {};
const hidden_loggers = ref(
  new Set<string>(JSON.parse(localStorage.getItem('hidden_loggers') || '[]'))
);
function initLoggersFilterStylesheet() {
  if (!iframe.value) {
    return;
  }
  if (stylesheet && stylesheet.ownerNode?.ownerDocument == iframe.value.contentWindow?.document) {
    // Stylesheet already initialized
    return;
  }
  const head = iframe.value.contentWindow?.document.head;
  if (head) {
    stylesheet = head.appendChild(document.createElement('style')).sheet as CSSStyleSheet;
  }
  rule_map = {};
  for (const logger of hidden_loggers.value) {
    const sanitized = sanitizeLoggerName(logger);
    rule_map[sanitized] = stylesheet!.insertRule(
      `.l-${sanitized} { display: none; }`,
      stylesheet!.cssRules.length
    );
  }
}
function toggleLogger(logger: string) {
  if (!iframe.value) {
    return;
  }
  initLoggersFilterStylesheet();

  const rule_selector = `.l-${logger}`;
  const rule_contents = `${rule_selector} { display: none; }`;
  if (rule_map[logger] !== undefined) {
    const rule = stylesheet!.cssRules[rule_map[logger]] as CSSStyleRule;
    if (hidden_loggers.value.has(logger)) {
      hidden_loggers.value.delete(logger);
      setTimeout(() => (rule.selectorText = '#none'), 0);
    } else {
      hidden_loggers.value.add(logger);
      setTimeout(() => (rule.selectorText = rule_selector), 0);
    }
  } else {
    hidden_loggers.value.add(logger);
    setTimeout(() => {
      rule_map[logger] = stylesheet!.insertRule(rule_contents, stylesheet!.cssRules.length);
    }, 0);
  }
  localStorage.setItem('hidden_loggers', JSON.stringify(Array.from(hidden_loggers.value)));
}

function keydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    router.back();
  }
}

function onMessage(e: MessageEvent) {
  if (e.origin === window.location.origin) {
    switch (e.data) {
      case 'close-logs-view':
        router.back();
        break;
      case 'logs-view-loaded':
        initLoggersFilterStylesheet();
        break;
      default:
        console.warn('Unknown message:', e.data);
    }
  }
}

// --- Loggers Menu ---
const loggers_menu_pos = ref<[number, number] | null>(null);
const loggers_list = ref<string[]>([]);
const toggle_loggers_btn = ref<HTMLButtonElement | null>(null);
function sanitizeLoggerName(name: string) {
  return name.replace(/[^a-zA-Z0-9-_]/g, '_');
}
function toggleLoggersMenu() {
  if (!iframe.value) {
    return;
  }
  const loggers = iframe.value.contentWindow?.document.querySelectorAll('.l');
  const loggers_by_key: { [sanitized: string]: string } = {};
  if (loggers) {
    loggers.forEach((logger) => {
      loggers_by_key[sanitizeLoggerName(logger.textContent!)] = logger.textContent!;
    });
  }
  const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
  loggers_list.value = Object.values(loggers_by_key).sort(collator.compare);

  if (loggers_menu_pos.value === null) {
    loggers_menu_pos.value = [
      Math.min(toggle_loggers_btn.value!.offsetLeft, innerWidth - 400) - 4,
      34
    ];
  } else {
    loggers_menu_pos.value = null;
  }
}

onMounted(() => {
  document.addEventListener('keydown', keydown);
  window.addEventListener('message', onMessage);
  document.title = `${route.params.test} (${route.params.run} - ${route.params.project}) · GreenDots`;
});
onUnmounted(() => {
  document.removeEventListener('keydown', keydown);
  window.removeEventListener('message', onMessage);
});
</script>

<template>
  <nav>
    <span class="project-name">{{ $route.params.project }}</span>
    <span class="run-name">{{ $route.params.run }}</span>
    <span class="test-name">{{ $route.params.test }}</span>
    <div class="spacer"></div>
    <div class="level-toggles">
      <button class="toggle-loggers" @click="toggleLoggersMenu()" ref="toggle_loggers_btn">
        Loggers{{ hidden_loggers.size > 0 ? ` (${hidden_loggers.size} hidden)` : '' }}
      </button>
      <label class="level-toggle" v-for="level in ['d', 'i', 'w', 'e', 'c', 't']" :key="level">
        <input
          type="checkbox"
          :checked="!hidden_log_levels.includes(level)"
          @change="(e) => toggleStatus(e, level)"
        />{{ labels[level] }}
      </label>
    </div>
    <!-- <span>TODO:Search</span>
    <span>TODO:Statistics</span>
    <span>TODO:Settings</span> -->
  </nav>
  <iframe
    class="logs"
    ref="iframe"
    :src="`/api/v1/projects/${encodeURIComponent($route.params.project)}/runs/${encodeURIComponent($route.params.run)}/test/${encodeURIComponent($route.params.test)}/log_stream`"
  ></iframe>
  <div class="loggers-menu-overlay" v-if="loggers_menu_pos" @click="loggers_menu_pos = null"></div>
  <ul
    class="loggers-menu"
    v-if="loggers_menu_pos"
    :style="{ top: loggers_menu_pos[1] + 'px', left: loggers_menu_pos[0] + 'px' }"
  >
    <li
      v-for="logger in loggers_list"
      :key="logger"
      :class="{ disabled: hidden_loggers.has(sanitizeLoggerName(logger)) }"
    >
      <label
        ><input
          type="checkbox"
          :checked="!hidden_loggers.has(sanitizeLoggerName(logger))"
          @change="toggleLogger(sanitizeLoggerName(logger))"
        />{{ logger }}
      </label>
    </li>
  </ul>
</template>

<style scoped>
nav {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 32px;
  display: flex;
  justify-content: start;
  gap: 1rem;
  background: #121212;
  font-size: 18px;
  z-index: 100;
  padding: 0 32px;
}
.level-toggles {
  display: flex;
  gap: 9px;
}
nav .test-name {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.spacer {
  flex-grow: 1;
}
.level-toggle {
  display: flex;
  gap: 3px;
  align-items: center;
  font-size: 14px;
}
nav a {
  color: #aeaeae;
}
.toggle-loggers {
  margin: 3px;
  font-size: 14px;
}
.loggers-menu {
  display: flex;
  flex-direction: column;
  gap: 1px;
  z-index: 102;
  position: fixed;
  min-width: 100px;
  max-width: 400px;
  max-height: calc(100vh - 36px - 8px);
  background: #1a1a1a;
  word-break: break-all;
  padding-inline-start: 0;
  border-radius: 6px;
  overflow: hidden;
  overflow-y: auto;
  border: 1px solid #333;
}
.loggers-menu li {
  cursor: pointer;
  list-style: none;
  background: #2a2a2a;
  user-select: none;
}
.loggers-menu li label {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 4px 12px;
}
.loggers-menu li:hover {
  background: #3a3a3a;
}
.loggers-menu li.disabled {
  color: #666;
  background: #121212;
}
.loggers-menu li.disabled:hover {
  background: #1a1a1a;
}
.loggers-menu-overlay {
  z-index: 101;
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  bottom: 0;
  background: rgba(0, 0, 0, 0.4);
}
.logs {
  position: fixed;
  top: 32px;
  left: 0;
  right: 0;
  bottom: 0;
  width: 100%;
  height: calc(100% - 32px);
  border: none;
}
</style>
