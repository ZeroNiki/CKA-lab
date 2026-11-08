// CKA Lab UI. Vue 3.5.45 (vendor/vue.esm-browser.prod.js), no build step.
import { createApp, reactive, ref, computed, onMounted, onUnmounted, watch } from "./vendor/vue.esm-browser.prod.js";

// ---------- API ----------

async function api(method, path, body) {
  const res = await fetch(path, {
    method,
    headers: body ? { "Content-Type": "application/json" } : {},
    body: body ? JSON.stringify(body) : undefined,
  });
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`);
  return data;
}

// ---------- shared state ----------

const store = reactive({
  coins: 0,
  breakUntil: null,
  busy: false,
  error: "",
  now: Date.now(),
});

async function refreshState() {
  try {
    const s = await api("GET", "/api/state");
    store.coins = s.coins;
    store.breakUntil = s.breakUntil ? new Date(s.breakUntil).getTime() : null;
    store.busy = s.busy;
  } catch (e) {
    store.error = e.message;
  }
}

function showError(e) {
  store.error = e.message || String(e);
}

const breakLeft = computed(() => (store.breakUntil ? Math.max(0, store.breakUntil - store.now) : 0));

function fmtTime(ms) {
  const s = Math.ceil(ms / 1000);
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`;
}

function beep() {
  try {
    const ctx = new AudioContext();
    const osc = ctx.createOscillator();
    osc.frequency.value = 880;
    osc.connect(ctx.destination);
    osc.start();
    osc.stop(ctx.currentTime + 0.4);
  } catch { /* no audio available */ }
}

// Escape HTML, then turn `code` into <code>. The statement keeps its line breaks via CSS.
function renderText(text) {
  const esc = (text || "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);
  return esc.replace(/`([^`]+)`/g, "<code>$1</code>");
}

// ---------- routing ----------

const route = ref(parseHash());
function parseHash() {
  const [path, query] = location.hash.replace(/^#/, "").split("?");
  return { parts: (path || "/").split("/").filter(Boolean), query: new URLSearchParams(query || "") };
}
window.addEventListener("hashchange", () => {
  route.value = parseHash();
  store.error = "";
});

// ---------- components ----------

const Home = {
  setup() {
    const levels = ref([]);
    onMounted(async () => {
      try { levels.value = await api("GET", "/api/levels"); } catch (e) { showError(e); }
    });
    return { levels };
  },
  template: `
    <h2>Levels</h2>
    <div class="grid">
      <div class="card" v-for="l in levels" :key="l.id">
        <h3><a :href="'#/level/' + l.id">{{ l.title }}</a></h3>
        <div class="muted">{{ l.domain }}</div>
        <div class="row" style="margin-top:8px">
          <span class="badge" :class="{ ok: l.solved === l.total }">{{ l.solved }}/{{ l.total }} solved</span>
          <span class="badge ok" v-if="l.theoryPassed">Theory passed</span>
        </div>
      </div>
    </div>
    <h2>Modes</h2>
    <div class="row">
      <a href="#/mixed"><button>Mixed</button></a>
      <a href="#/practice"><button>Practice only</button></a>
      <a href="#/shop"><button>Shop</button></a>
    </div>`,
};

const Level = {
  props: ["id"],
  setup(props) {
    const level = ref(null);
    onMounted(async () => {
      try {
        const all = await api("GET", "/api/levels");
        level.value = all.find((l) => l.id === props.id) || null;
        if (!level.value) store.error = `Level ${props.id} not found`;
      } catch (e) { showError(e); }
    });
    return { level };
  },
  template: `
    <div v-if="level">
      <p><a href="#/">← Levels</a></p>
      <h2>{{ level.title }}</h2>
      <div class="card" v-if="!level.theoryPassed">
        <p>Pass the theory first (10 questions, at most 1 mistake) to unlock the practice tasks.</p>
        <a :href="'#/level/' + level.id + '/theory'"><button class="primary">Start theory</button></a>
      </div>
      <div class="card">
        <ul class="tasks">
          <li v-for="(t, i) in level.tasks" :key="t.id">
            <span class="muted">{{ i + 1 }}.</span>
            <a v-if="level.theoryPassed" :href="'#/task/' + t.id + '?mode=levels'">{{ t.title }}</a>
            <span v-else class="muted">{{ t.title }}</span>
            <span class="badge" :class="t.difficulty">{{ t.difficulty }}</span>
            <span class="badge snap" v-if="t.requiresSnapshot" title="Breaks the cluster; restored from snapshot afterwards">snapshot</span>
            <span class="badge ok" v-if="t.solved">solved</span>
          </li>
        </ul>
      </div>
    </div>`,
};

// One theory question with answer feedback; used by Theory and Mixed.
const QuestionCard = {
  props: ["question", "result", "disabled"],
  emits: ["answer"],
  setup() { return { renderText }; },
  template: `
    <div class="card">
      <div class="statement" v-html="renderText(question.question)"></div>
      <div class="options">
        <button v-for="(o, i) in question.options" :key="i"
          :disabled="disabled || !!result"
          :class="{ right: result && i === result.correctOption, wrong: result && !result.correct && i === result.chosen }"
          @click="$emit('answer', i)" v-html="renderText(o)"></button>
      </div>
      <div v-if="result">
        <strong :class="result.correct ? 'pass' : 'fail'" :style="{ color: result.correct ? 'var(--ok)' : 'var(--bad)' }">
          {{ result.correct ? 'Correct' : 'Wrong' }}
        </strong>
        <div class="statement muted" v-html="renderText(result.explanation)"></div>
      </div>
    </div>`,
};

const Theory = {
  props: ["id"],
  components: { QuestionCard },
  setup(props) {
    const quiz = ref(null);
    const index = ref(0);
    const result = ref(null);
    const final = ref(null);

    async function start() {
      final.value = null;
      result.value = null;
      index.value = 0;
      try { quiz.value = await api("POST", `/api/levels/${props.id}/theory`); } catch (e) { showError(e); }
    }
    async function answer(option) {
      const q = quiz.value.questions[index.value];
      try {
        const r = await api("POST", `/api/quiz/${quiz.value.quizId}/answer`, { questionId: q.id, option });
        result.value = { ...r, chosen: option };
        if (r.done) final.value = r;
      } catch (e) { showError(e); }
    }
    function next() {
      result.value = null;
      index.value++;
    }
    onMounted(start);
    return { quiz, index, result, final, start, answer, next };
  },
  template: `
    <p><a :href="'#/level/' + id">← Level</a></p>
    <h2>Theory</h2>
    <div v-if="quiz && quiz.questions[index]">
      <p class="muted">Question {{ index + 1 }} / {{ quiz.questions.length }} · mistakes: {{ result ? result.wrong : (final ? final.wrong : '') }}</p>
      <QuestionCard :question="quiz.questions[index]" :result="result" @answer="answer" />
      <div class="row" style="margin-top:12px">
        <button v-if="result && !final" class="primary" @click="next">Next</button>
      </div>
    </div>
    <div class="card" v-if="final" style="margin-top:12px">
      <div v-if="final.passed">
        <h3>Passed with {{ final.wrong }} mistake(s)</h3>
        <a :href="'#/level/' + id"><button class="primary">Go to practice</button></a>
      </div>
      <div v-else>
        <h3>Failed: {{ final.wrong }} mistakes</h3>
        <p>Questions and options will be shuffled.</p>
        <button class="primary" @click="start">Retry</button>
      </div>
    </div>`,
};

const Task = {
  props: ["id", "mode"],
  emits: ["solved"],
  setup(props, { emit }) {
    const task = ref(null);
    const started = ref(false);
    const working = ref(false);
    const outcome = ref(null);

    async function load() {
      try { task.value = await api("GET", `/api/tasks/${props.id}`); } catch (e) { showError(e); }
    }
    async function start() {
      if (task.value.requiresSnapshot &&
          !confirm("This task breaks the cluster. After Check (pass) or Give up, all nodes are restored from the snapshot. Continue?")) return;
      working.value = true;
      outcome.value = null;
      try {
        await api("POST", `/api/tasks/${props.id}/start`, { mode: props.mode });
        started.value = true;
      } catch (e) { showError(e); }
      working.value = false;
    }
    async function check() {
      working.value = true;
      try {
        outcome.value = await api("POST", `/api/tasks/${props.id}/check`);
        if (outcome.value.pass) {
          started.value = false;
          await load();
          emit("solved");
        }
      } catch (e) { showError(e); }
      working.value = false;
      refreshState();
    }
    async function reveal() {
      if (!confirm("No coins for this task if you open the solution now. Continue?")) return;
      try {
        const r = await api("POST", `/api/tasks/${props.id}/reveal`);
        task.value = { ...task.value, ...r };
      } catch (e) { showError(e); }
    }
    async function giveUp() {
      working.value = true;
      try {
        await api("POST", `/api/tasks/${props.id}/giveup`);
        started.value = false;
      } catch (e) { showError(e); }
      working.value = false;
    }
    watch(() => props.id, () => { started.value = false; outcome.value = null; load(); });
    onMounted(load);
    return { task, started, working, outcome, start, check, reveal, giveUp, renderText, store };
  },
  template: `
    <div class="card" v-if="task">
      <div class="row">
        <h3 style="margin:0">{{ task.title }}</h3>
        <span class="badge" :class="task.difficulty">{{ task.difficulty }}</span>
        <span class="badge snap" v-if="task.requiresSnapshot">snapshot</span>
      </div>
      <div class="statement" style="margin:12px 0" v-html="renderText(task.statement)"></div>
      <p class="muted">Work on the control plane: <code>./lab ssh</code></p>
      <div class="row">
        <button class="primary" @click="start" :disabled="working || store.busy">{{ started ? 'Restart' : 'Start' }}</button>
        <button @click="check" :disabled="!started || working || store.busy">Check</button>
        <button @click="reveal" v-if="!task.solution" :disabled="working">Show solution</button>
        <button @click="giveUp" v-if="started && task.requiresSnapshot" :disabled="working || store.busy">Give up</button>
        <span class="muted" v-if="working">{{ task.requiresSnapshot && outcome && outcome.pass ? 'Restoring cluster…' : 'Working…' }}</span>
      </div>
      <div v-if="outcome" style="margin-top:12px">
        <ul class="results">
          <li v-for="r in outcome.results" :class="r.pass ? 'pass' : 'fail'">
            {{ r.pass ? '✓' : '✗' }} {{ r.description }}<span v-if="!r.pass && r.actual" class="muted"> — got: {{ r.actual }}</span>
          </li>
        </ul>
        <p v-if="outcome.pass"><strong>Solved! +{{ outcome.coins }} coins</strong>
          <span v-if="outcome.bonus"> · level completed, +bonus</span></p>
        <p v-if="outcome.restoreError" class="banner error">Snapshot restore failed: {{ outcome.restoreError }}</p>
      </div>
      <div v-if="task.solution" style="margin-top:12px">
        <p v-if="task.docs">Docs: <a :href="task.docs" target="_blank" rel="noopener">{{ task.docs }}</a></p>
        <pre>{{ task.solution }}</pre>
      </div>
    </div>`,
};

const TaskPage = {
  props: ["id", "mode"],
  components: { Task },
  setup() {
    return {};
  },
  template: `
    <p><a v-if="mode === 'levels'" href="javascript:history.back()">← Back</a><a v-else href="#/">← Home</a></p>
    <Task :id="id" :mode="mode" />`,
};

const Stream = {
  props: ["practiceOnly"],
  components: { QuestionCard, Task },
  setup(props) {
    const includeSnapshot = ref(false);
    const item = ref(null);
    const result = ref(null);
    async function next() {
      result.value = null;
      try {
        item.value = await api("GET", `/api/mixed/next?practice=${props.practiceOnly ? 1 : 0}&snapshot=${includeSnapshot.value ? 1 : 0}`);
      } catch (e) { showError(e); }
    }
    async function answer(option) {
      try {
        const r = await api("POST", "/api/mixed/answer", { questionId: item.value.question.id, option });
        result.value = { ...r, chosen: option };
      } catch (e) { showError(e); }
    }
    onMounted(next);
    return { includeSnapshot, item, result, next, answer };
  },
  template: `
    <p><a href="#/">← Home</a></p>
    <h2>{{ practiceOnly ? 'Practice only' : 'Mixed' }}</h2>
    <div class="row" style="margin-bottom:12px">
      <label><input type="checkbox" v-model="includeSnapshot"> Include cluster-breaking tasks</label>
      <button @click="next">Next</button>
    </div>
    <div v-if="item">
      <QuestionCard v-if="item.kind === 'question'" :question="item.question" :result="result" @answer="answer" />
      <Task v-else :id="item.taskId" :mode="practiceOnly ? 'practice' : 'mixed'" />
    </div>`,
};

const Shop = {
  setup() {
    const items = ref([]);
    onMounted(async () => {
      try { items.value = await api("GET", "/api/shop"); } catch (e) { showError(e); }
    });
    async function buy(it) {
      if ("Notification" in window && Notification.permission === "default") {
        Notification.requestPermission();
      }
      try {
        const r = await api("POST", `/api/shop/${it.id}/buy`);
        store.breakUntil = new Date(r.breakUntil).getTime();
        refreshState();
      } catch (e) { showError(e); }
    }
    return { items, buy, store, breakLeft, fmtTime };
  },
  template: `
    <p><a href="#/">← Home</a></p>
    <h2>Shop</h2>
    <div class="card" v-if="breakLeft > 0" style="margin-bottom:14px">
      <div class="muted">Break in progress</div>
      <div class="timer">{{ fmtTime(breakLeft) }}</div>
    </div>
    <div class="grid">
      <div class="card" v-for="it in items" :key="it.id">
        <h3>{{ it.title }}</h3>
        <p class="muted">{{ it.minutes }} min · {{ it.price }} coins</p>
        <button class="primary" @click="buy(it)" :disabled="breakLeft > 0">Buy</button>
      </div>
    </div>`,
};

const App = {
  components: { Home, Level, Theory, TaskPage, Stream, Shop },
  setup() {
    let tick, poll, wasOnBreak = false;
    onMounted(() => {
      refreshState();
      tick = setInterval(() => {
        store.now = Date.now();
        const onBreak = breakLeft.value > 0;
        if (wasOnBreak && !onBreak) {
          beep();
          if ("Notification" in window && Notification.permission === "granted") {
            new Notification("CKA Lab", { body: "Break is over. Back to kubectl!" });
          }
        }
        wasOnBreak = onBreak;
      }, 1000);
      poll = setInterval(refreshState, 5000);
    });
    onUnmounted(() => { clearInterval(tick); clearInterval(poll); });

    const view = computed(() => {
      const p = route.value.parts;
      if (p[0] === "level" && p[2] === "theory") return { c: "Theory", props: { id: p[1] } };
      if (p[0] === "level") return { c: "Level", props: { id: p[1] } };
      if (p[0] === "task") return { c: "TaskPage", props: { id: p[1], mode: route.value.query.get("mode") || "practice" } };
      if (p[0] === "mixed") return { c: "Stream", props: { practiceOnly: false } };
      if (p[0] === "practice") return { c: "Stream", props: { practiceOnly: true } };
      if (p[0] === "shop") return { c: "Shop", props: {} };
      return { c: "Home", props: {} };
    });
    const viewKey = computed(() => location.hash);
    return { store, view, viewKey, breakLeft, fmtTime };
  },
  template: `
    <header>
      <a class="brand" href="#/">CKA Lab</a>
      <span class="spacer"></span>
      <span v-if="breakLeft > 0" class="badge ok">Break {{ fmtTime(breakLeft) }}</span>
      <span class="coins">{{ store.coins }} coins</span>
      <a href="#/shop">Shop</a>
    </header>
    <main>
      <div class="banner info" v-if="store.busy">Restoring cluster from snapshot… tasks are paused.</div>
      <div class="banner error" v-if="store.error">{{ store.error }}<button @click="store.error = ''">×</button></div>
      <component :is="view.c" v-bind="view.props" :key="viewKey" />
    </main>`,
};

createApp(App).mount("#app");
