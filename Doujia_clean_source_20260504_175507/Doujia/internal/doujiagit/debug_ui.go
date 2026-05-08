package doujiagit

const debugUIHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>DoujiaGit Run Viewer</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f9;
      --panel: #ffffff;
      --line: #d7dce2;
      --text: #17202a;
      --muted: #65717f;
      --accent: #1769aa;
      --ok: #237a4b;
      --warn: #a15c00;
      --bad: #b3261e;
      --chip: #eef3f8;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font: 14px/1.45 system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    header {
      position: sticky;
      top: 0;
      z-index: 2;
      background: #ffffff;
      border-bottom: 1px solid var(--line);
      padding: 14px 20px;
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 16px;
    }
    h1, h2, h3 { margin: 0; letter-spacing: 0; }
    h1 { font-size: 18px; }
    h2 { font-size: 15px; margin-bottom: 10px; }
    h3 { font-size: 14px; margin-bottom: 8px; }
    button, select {
      border: 1px solid var(--line);
      background: #fff;
      color: var(--text);
      border-radius: 6px;
      min-height: 34px;
      padding: 0 10px;
      font: inherit;
    }
    button { cursor: pointer; }
    button.primary {
      background: var(--accent);
      border-color: var(--accent);
      color: white;
    }
    button.small {
      min-height: 28px;
      padding: 0 8px;
      font-size: 12px;
    }
    main {
      padding: 18px 20px 28px;
      display: grid;
      grid-template-columns: minmax(280px, 380px) minmax(0, 1fr);
      gap: 16px;
    }
    section {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 14px;
      min-width: 0;
    }
    .toolbar {
      display: flex;
      gap: 8px;
      align-items: center;
      flex-wrap: wrap;
    }
    .toggle {
      display: inline-flex;
      align-items: center;
      gap: 6px;
      min-height: 34px;
      color: var(--muted);
    }
    .live-dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: var(--line);
    }
    .live-dot.on { background: var(--ok); }
    .grid {
      display: grid;
      grid-template-columns: repeat(4, minmax(120px, 1fr));
      gap: 10px;
      margin-bottom: 16px;
    }
    .metric {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 12px;
    }
    .metric .value { font-size: 22px; font-weight: 700; }
    .metric .label { color: var(--muted); }
    .stack { display: grid; gap: 12px; }
    .item {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 10px;
      background: #fff;
    }
    .item.active { border-color: var(--accent); box-shadow: 0 0 0 2px rgba(23, 105, 170, .12); }
    .row {
      display: flex;
      align-items: flex-start;
      justify-content: space-between;
      gap: 10px;
      margin-top: 6px;
    }
    .muted { color: var(--muted); }
    .mono {
      font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
      overflow-wrap: anywhere;
    }
    .chips {
      display: flex;
      flex-wrap: wrap;
      gap: 6px;
      margin-top: 8px;
    }
    .chip {
      display: inline-flex;
      align-items: center;
      min-height: 24px;
      padding: 2px 8px;
      border-radius: 999px;
      background: var(--chip);
      color: #253445;
      max-width: 100%;
    }
    .chip.ok { background: #e7f4ec; color: var(--ok); }
    .chip.warn { background: #fff2db; color: var(--warn); }
    .chip.bad { background: #fdebea; color: var(--bad); }
    .flow {
      display: flex;
      gap: 8px;
      overflow-x: auto;
      padding-bottom: 4px;
    }
    .flow-step {
      min-width: 132px;
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 10px;
      background: #fff;
      position: relative;
    }
    .flow-step::after {
      content: "›";
      position: absolute;
      right: -10px;
      top: 50%;
      transform: translateY(-50%);
      color: var(--muted);
      font-size: 22px;
    }
    .flow-step:last-child::after { content: ""; }
    .flow-step.done { border-color: #b9dfc7; background: #f4fbf7; }
    .flow-step.warn { border-color: #f0cf99; background: #fff8ed; }
    .flow-step.todo { background: #fafbfc; }
    .flow-title { font-weight: 700; }
    .flow-sub { color: var(--muted); margin-top: 4px; font-size: 12px; }
    .module-grid {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
      gap: 10px;
    }
    .module-card {
      border: 1px solid var(--line);
      border-radius: 8px;
      padding: 12px;
      background: #fff;
    }
    .module-card.warn { border-color: #f0cf99; background: #fff8ed; }
    .module-title {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 8px;
      margin-bottom: 8px;
    }
    .task-pills {
      display: grid;
      gap: 6px;
    }
    .task-pill {
      border: 1px solid var(--line);
      border-radius: 6px;
      padding: 7px 8px;
      display: grid;
      grid-template-columns: minmax(0, 1fr) auto;
      gap: 8px;
      align-items: center;
      cursor: pointer;
      background: #fff;
    }
    .task-pill.done { border-color: #b9dfc7; }
    .task-pill.warn { border-color: #f0cf99; }
    .task-pill.todo { color: var(--muted); background: #fafbfc; cursor: default; }
    .short-id {
      font-family: ui-monospace, SFMono-Regular, Consolas, "Liberation Mono", monospace;
      cursor: help;
      overflow-wrap: anywhere;
    }
    .timeline {
      position: relative;
      display: grid;
      gap: 10px;
    }
    .move {
      border-left: 3px solid var(--line);
      padding-left: 10px;
    }
    .move.recover { border-left-color: var(--warn); }
    .move.advance { border-left-color: var(--ok); }
    .table {
      width: 100%;
      border-collapse: collapse;
      table-layout: fixed;
    }
    .table th, .table td {
      border-bottom: 1px solid var(--line);
      padding: 8px 6px;
      text-align: left;
      vertical-align: top;
    }
    .table th {
      color: var(--muted);
      font-weight: 600;
      background: #fafbfc;
    }
    .table td { overflow-wrap: anywhere; white-space: pre-line; }
    .split {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(280px, 420px);
      gap: 16px;
    }
    pre {
      margin: 8px 0 0;
      padding: 10px;
      background: #f3f5f7;
      border: 1px solid var(--line);
      border-radius: 6px;
      overflow: auto;
      max-height: 260px;
      white-space: pre-wrap;
    }
    .error {
      background: #fdebea;
      color: var(--bad);
      border: 1px solid #f5c2bd;
      padding: 10px;
      border-radius: 8px;
      margin-bottom: 12px;
    }
    @media (max-width: 1000px) {
      main, .split { grid-template-columns: 1fr; }
      .grid { grid-template-columns: repeat(2, minmax(120px, 1fr)); }
      header { align-items: flex-start; flex-direction: column; }
    }
  </style>
</head>
<body>
  <header>
    <div>
      <h1>DoujiaGit Run Viewer</h1>
      <div id="run-title" class="muted mono"></div>
    </div>
    <div class="toolbar">
      <select id="ref-select" aria-label="ref">
        <option value="main">main</option>
      </select>
      <label class="toggle"><input id="auto-refresh" type="checkbox" checked> 自动刷新</label>
      <span id="live-dot" class="live-dot on"></span>
      <span id="last-refresh" class="muted"></span>
      <button id="refresh" class="primary">刷新</button>
      <button id="open-json">打开 JSON</button>
    </div>
  </header>

  <main>
    <aside class="stack">
      <section>
        <h2>当前 Ref</h2>
        <div id="ref-panel"></div>
      </section>
      <section>
        <h2>Ref 移动</h2>
        <div id="moves" class="timeline"></div>
      </section>
      <section>
        <h2>Frontier</h2>
        <div id="frontiers" class="stack"></div>
      </section>
    </aside>

    <div>
      <div id="error"></div>
      <div class="grid">
        <div class="metric"><div id="metric-snapshots" class="value">0</div><div class="label">Task Snapshots</div></div>
        <div class="metric"><div id="metric-bags" class="value">0</div><div class="label">Artifact Bags</div></div>
        <div class="metric"><div id="metric-frontiers" class="value">0</div><div class="label">Frontiers</div></div>
        <div class="metric"><div id="metric-moves" class="value">0</div><div class="label">Ref Moves</div></div>
        <div class="metric"><div id="metric-decisions" class="value">0</div><div class="label">Processing Decisions</div></div>
      </div>

      <section style="margin-bottom:16px">
        <h2>交付流程总览</h2>
        <div id="flow" class="flow"></div>
      </section>

      <section style="margin-bottom:16px">
        <h2>模块并行视图</h2>
        <div id="modules" class="module-grid"></div>
      </section>

      <section style="margin-bottom:16px">
        <h2>Recover 事件</h2>
        <div id="recoveries" class="stack"></div>
      </section>

      <div class="split">
        <section>
          <h2>Task Snapshot 时间线</h2>
          <table class="table">
            <thead>
              <tr>
                <th style="width: 22%">Task</th>
                <th style="width: 10%">Result</th>
                <th style="width: 28%">Input Bags</th>
                <th style="width: 28%">Output Bags</th>
                <th style="width: 12%">Time</th>
              </tr>
            </thead>
            <tbody id="snapshots"></tbody>
          </table>
        </section>
        <section>
          <h2>选中详情</h2>
          <div id="detail" class="muted">点击左侧 ref move、frontier 或 task 查看细节。</div>
        </section>
      </div>

      <section style="margin-top:16px">
        <h2>Bag 产物关系</h2>
        <table class="table">
          <thead>
            <tr>
              <th style="width: 24%">Bag</th>
              <th style="width: 24%">Produced By</th>
              <th style="width: 28%">Consumed By</th>
              <th style="width: 24%">Versions</th>
            </tr>
          </thead>
          <tbody id="bags"></tbody>
        </table>
      </section>
    </div>
  </main>

  <script>
    const state = { graph: null, loading: false };
    const parts = window.location.pathname.split("/").filter(Boolean);
    const runID = parts[3] || "";
    const base = "/debug/doujiagit/runs/" + encodeURIComponent(runID);
    const title = document.getElementById("run-title");
    title.textContent = "run_id: " + runID;

    document.getElementById("refresh").addEventListener("click", load);
    document.getElementById("open-json").addEventListener("click", function () {
      window.open(base + "/graph?ref=" + encodeURIComponent(currentRef()), "_blank");
    });
    document.getElementById("ref-select").addEventListener("change", load);
    document.getElementById("auto-refresh").addEventListener("change", syncAutoRefresh);

    function currentRef() {
      return document.getElementById("ref-select").value || "main";
    }

    async function load() {
      if (state.loading) return;
      state.loading = true;
      showError("");
      try {
        const res = await fetch(base + "/graph?ref=" + encodeURIComponent(currentRef()), { headers: { "Accept": "application/json" } });
        if (!res.ok) throw new Error(await res.text());
        state.graph = await res.json();
        render(state.graph);
        text("last-refresh", "更新于 " + new Date().toLocaleTimeString());
      } catch (err) {
        showError(err.message || String(err));
      } finally {
        state.loading = false;
      }
    }

    function render(graph) {
      renderRefOptions(graph.refs || [], graph.ref || {});
      text("metric-snapshots", (graph.snapshots || []).length);
      text("metric-bags", (graph.bags || []).length);
      text("metric-frontiers", (graph.history_frontier_snapshots || graph.frontier_snapshots || []).length);
      text("metric-moves", (graph.ref_move_events || []).length);
      renderRef(graph.ref || {});
      renderMoves(graph.ref_move_events || []);
      renderFrontiers(graph.history_frontier_snapshots || graph.frontier_snapshots || [], graph.ref || {});
      renderFlow(graph.snapshots || []);
      renderModules(graph.snapshots || []);
      renderRecoveries(graph.ref_move_events || []);
      text("metric-decisions", (graph.processing_decisions || []).length);
      renderSnapshots(graph.snapshots || []);
      renderBags(graph.bags || []);
    }

    function renderRefOptions(refs, current) {
      const select = document.getElementById("ref-select");
      const selected = currentRef();
      const names = refs.map(function (ref) { return ref.ref_name; });
      if (!names.includes(current.ref_name || selected)) names.unshift(current.ref_name || selected || "main");
      const nextHTML = names.map(function (name) {
        return '<option value="' + escapeAttr(name) + '">' + escapeHTML(name) + '</option>';
      }).join("");
      if (select.innerHTML !== nextHTML) select.innerHTML = nextHTML;
      select.value = current.ref_name || selected || "main";
    }

    function renderRef(ref) {
      const panel = document.getElementById("ref-panel");
      panel.innerHTML = "";
      panel.appendChild(el("div", "mono", ref.ref_name || "main"));
      panel.appendChild(row("current frontier", shortID(ref.frontier_snapshot_id || "-"), ref.frontier_snapshot_id));
      panel.appendChild(row("frontier members", (ref.frontier_member_snapshot_ids || ref.frontier_snapshot_ids || []).map(shortID).join(", ") || "-", (ref.frontier_member_snapshot_ids || ref.frontier_snapshot_ids || []).join(", ")));
      panel.appendChild(row("legacy member field", (ref.frontier_snapshot_ids || []).map(shortID).join(", ") || "-", (ref.frontier_snapshot_ids || []).join(", ")));
      panel.appendChild(row("updated", formatTime(ref.updated_at)));
    }

    function renderMoves(moves) {
      const box = document.getElementById("moves");
      box.innerHTML = "";
      if (!moves.length) {
        box.appendChild(el("div", "muted", "暂无 ref move。"));
        return;
      }
      moves.forEach(function (move) {
        const item = el("div", "move " + (move.mode || ""));
        const label = el("div", "row");
        label.appendChild(el("strong", "", move.mode || "move"));
        label.appendChild(chip(formatTime(move.created_at), ""));
        item.appendChild(label);
        item.appendChild(row("to", (move.to_frontier_snapshot_ids || []).map(shortID).join(", "), (move.to_frontier_snapshot_ids || []).join(", ")));
        if (move.reason) item.appendChild(row("reason", move.reason));
        item.addEventListener("click", function () { showDetail("Ref Move", move); });
        box.appendChild(item);
      });
    }

    function renderFrontiers(frontiers, ref) {
      const box = document.getElementById("frontiers");
      box.innerHTML = "";
      if (!frontiers.length) {
        box.appendChild(el("div", "muted", "暂无 frontier。"));
        return;
      }
      frontiers.forEach(function (frontier) {
        const item = el("div", "item" + (frontier.frontier_snapshot_id === ref.frontier_snapshot_id ? " active" : ""));
        const head = el("div", "row");
        head.appendChild(el("div", "short-id", shortID(frontier.frontier_snapshot_id)));
        const checkout = el("button", "small", "设为当前");
        checkout.disabled = frontier.frontier_snapshot_id === ref.frontier_snapshot_id;
        checkout.addEventListener("click", function (event) {
          event.stopPropagation();
          checkoutFrontier(frontier.frontier_snapshot_id);
        });
        head.appendChild(checkout);
        item.appendChild(head);
        item.title = frontier.frontier_snapshot_id;
        item.appendChild(row("mode", frontier.created_by_mode || "-"));
        item.appendChild(row("tasks", (frontier.task_snapshot_ids || []).length));
        item.appendChild(row("time", formatTime(frontier.created_at)));
        item.addEventListener("click", function () { showDetail("Frontier", frontier); });
        box.appendChild(item);
      });
    }

    async function checkoutFrontier(frontierID) {
      showError("");
      try {
        const res = await fetch(base + "/refs/" + encodeURIComponent(currentRef()), {
          method: "POST",
          headers: { "Content-Type": "application/json", "Accept": "application/json" },
          body: JSON.stringify({ frontier_snapshot_id: frontierID, reason: "debug ui checkout" })
        });
        if (!res.ok) throw new Error(await res.text());
        await load();
      } catch (err) {
        showError(err.message || String(err));
      }
    }

    function syncAutoRefresh() {
      const enabled = document.getElementById("auto-refresh").checked;
      document.getElementById("live-dot").className = "live-dot" + (enabled ? " on" : "");
      if (state.timer) {
        clearInterval(state.timer);
        state.timer = null;
      }
      if (enabled) {
        state.timer = setInterval(load, 2000);
      }
    }

    function renderFlow(snapshots) {
      const box = document.getElementById("flow");
      box.innerHTML = "";
      flowStages().forEach(function (stage) {
        const status = stageStatus(stage, snapshots);
        const node = el("div", "flow-step " + status.className);
        node.appendChild(el("div", "flow-title", stage.title));
        node.appendChild(el("div", "flow-sub", status.text));
        node.addEventListener("click", function () {
          showDetail(stage.title, { stage: stage, matched_tasks: matchingSnapshots(stage, snapshots) });
        });
        box.appendChild(node);
      });
    }

    function renderModules(snapshots) {
      const box = document.getElementById("modules");
      box.innerHTML = "";
      const modules = moduleGroups(snapshots);
      const keys = Object.keys(modules).sort();
      if (!keys.length) {
        box.appendChild(el("div", "muted", "暂无模块任务。"));
        return;
      }
      keys.forEach(function (key) {
        const tasks = modules[key];
        const hasBug = tasks.some(function (task) { return task.result === "kbug"; });
        const card = el("div", "module-card" + (hasBug ? " warn" : ""));
        const title = el("div", "module-title");
        title.appendChild(el("strong", "", key));
        title.appendChild(chip(hasBug ? "需恢复" : "交付中/完成", hasBug ? "warn" : "ok"));
        card.appendChild(title);
        const list = el("div", "task-pills");
        ["write_code", "test_data", "test_code"].forEach(function (kind) {
          const task = tasks.find(function (item) { return taskInfo(item.task_id).kind === kind; });
          list.appendChild(moduleTaskPill(kind, task));
        });
        card.appendChild(list);
        box.appendChild(card);
      });
    }

    function renderRecoveries(moves) {
      const box = document.getElementById("recoveries");
      box.innerHTML = "";
      const recoveries = moves.filter(function (move) { return move.mode === "recover"; });
      if (!recoveries.length) {
        box.appendChild(el("div", "muted", "本次 run 没有 recover。出现 kbug 后这里会显示 debug_code 和保留/替换的 bag。"));
        return;
      }
      recoveries.forEach(function (move) {
        const details = parseDetails(move.details_json);
        const item = el("div", "item");
        item.appendChild(el("strong", "", "Recover: " + (details.failed_task_id || move.reason || move.event_id)));
        item.appendChild(row("debug task", details.debug_task_id || "-"));
        item.appendChild(row("keep bags", (details.keep_bag_ids || []).map(shortID).join(", ") || "-", (details.keep_bag_ids || []).join(", ")));
        item.appendChild(row("failure report", (details.failure_report_bag_ids || []).map(shortID).join(", ") || "-", (details.failure_report_bag_ids || []).join(", ")));
        item.addEventListener("click", function () { showDetail("Recover Event", move); });
        box.appendChild(item);
      });
    }

    function renderSnapshots(snapshots) {
      const tbody = document.getElementById("snapshots");
      tbody.innerHTML = "";
      snapshots.forEach(function (snapshot) {
        const tr = document.createElement("tr");
        const info = taskInfo(snapshot.task_id);
        tr.appendChild(td(info.label + "\n" + snapshot.task_id, "mono"));
        tr.appendChild(tdNode(resultChip(snapshot.result)));
        tr.appendChild(td((snapshot.input_bag_ids || []).map(shortID).join("\n"), "mono", (snapshot.input_bag_ids || []).join("\n")));
        tr.appendChild(td((snapshot.output_bag_ids || []).map(shortID).join("\n"), "mono", (snapshot.output_bag_ids || []).join("\n")));
        tr.appendChild(td(formatTime(snapshot.created_at)));
        tr.addEventListener("click", function () { showDetail("Task Snapshot", snapshot); });
        tbody.appendChild(tr);
      });
    }

    function renderBags(bags) {
      const tbody = document.getElementById("bags");
      tbody.innerHTML = "";
      bags.forEach(function (bag) {
        const tr = document.createElement("tr");
        tr.appendChild(td(shortID(bag.bag_id), "mono", bag.bag_id));
        tr.appendChild(td(shortID(bag.producer_snapshot_id || "-"), "mono", bag.producer_snapshot_id || "-"));
        tr.appendChild(td((bag.consumer_snapshot_ids || []).map(shortID).join("\n") || "-", "mono", (bag.consumer_snapshot_ids || []).join("\n") || "-"));
        tr.appendChild(td((bag.artifact_version_ids || []).map(shortID).join("\n"), "mono", (bag.artifact_version_ids || []).join("\n")));
        tr.addEventListener("click", function () { showDetail("Artifact Bag", bag); });
        tbody.appendChild(tr);
      });
    }

    function flowStages() {
      return [
        { title: "CEO 需求", match: function (info) { return info.kind === "requirement"; } },
        { title: "PM 方案", match: function (info) { return info.kind === "prd"; } },
        { title: "架构设计", match: function (info) { return info.kind === "architecture"; } },
        { title: "容器准备", match: function (info) { return info.kind === "create_container"; } },
        { title: "模块拆分", match: function (info) { return info.kind === "split"; } },
        { title: "模块并行交付", match: function (info) { return info.module !== ""; } },
        { title: "代码合并", match: function (info) { return info.kind === "merge_code"; } },
        { title: "全局测试数据", match: function (info) { return info.kind === "global_test_data"; } },
        { title: "全局测试", match: function (info) { return info.kind === "global_test_code"; } }
      ];
    }

    function stageStatus(stage, snapshots) {
      const matched = matchingSnapshots(stage, snapshots);
      if (!matched.length) return { className: "todo", text: "等待中" };
      if (matched.some(function (snapshot) { return snapshot.result === "kbug"; })) return { className: "warn", text: "发现 bug" };
      if (matched.every(function (snapshot) { return snapshot.result === "kok"; })) return { className: "done", text: matched.length + " 个任务完成" };
      return { className: "warn", text: matched.length + " 个任务有结果" };
    }

    function matchingSnapshots(stage, snapshots) {
      return snapshots.filter(function (snapshot) {
        return stage.match(taskInfo(snapshot.task_id));
      });
    }

    function moduleGroups(snapshots) {
      const groups = {};
      snapshots.forEach(function (snapshot) {
        const info = taskInfo(snapshot.task_id);
        if (!info.module) return;
        if (!groups[info.module]) groups[info.module] = [];
        groups[info.module].push(snapshot);
      });
      return groups;
    }

    function moduleTaskPill(kind, snapshot) {
      const names = { write_code: "写代码", test_data: "写测试数据", test_code: "模块测试" };
      if (!snapshot) {
        const pill = el("div", "task-pill todo");
        pill.appendChild(el("span", "", names[kind] || kind));
        pill.appendChild(chip("等待", ""));
        return pill;
      }
      const cls = snapshot.result === "kok" ? "done" : (snapshot.result === "kbug" ? "warn" : "");
      const pill = el("div", "task-pill " + cls);
      pill.appendChild(el("span", "", names[kind] || kind));
      pill.appendChild(resultChip(snapshot.result));
      pill.addEventListener("click", function () { showDetail("Task Snapshot", snapshot); });
      return pill;
    }

    function taskInfo(taskID) {
      const id = String(taskID || "");
      const explicitModuleMatch = id.match(/module(\d+)/i);
      const agentModuleMatch = id.match(/(?:coder|tester)(\d+)/i);
      const moduleMatch = explicitModuleMatch || agentModuleMatch;
      const module = moduleMatch ? "module" + moduleMatch[1].padStart(2, "0") : "";
      if (id === "task_01" || id === "ceo_write_requirement") return { label: "CEO 写需求", kind: "requirement", module: "" };
      if (id === "task_02" || id === "pm_write_plan") return { label: "PM 写方案", kind: "prd", module: "" };
      if (id === "task_03" || id === "ceo_review_product_plan") return { label: "CEO 审方案", kind: "review_prd", module: "" };
      if (id === "task_04" || id === "architect_write_plan") return { label: "架构师设计", kind: "architecture", module: "" };
      if (id === "task_05" || id === "pm_review_architecture") return { label: "PM 审架构", kind: "review_architecture", module: "" };
      if (id === "architect_create_container" || id.includes("_create_container")) return { label: "架构师准备容器", kind: "create_container", module: "" };
      if (id === "task_06" || id === "split_module") return { label: "架构师拆分模块", kind: "split", module: "" };
      if (id.includes("_write_code")) return { label: (module || "模块") + " 写代码", kind: "write_code", module: module };
      if (id.includes("_test_data") && !id.includes("_global_test_data")) return { label: (module || "模块") + " 写测试数据", kind: "test_data", module: module };
      if (id.includes("_test_code") && !id.includes("_global_test_code")) return { label: (module || "模块") + " 模块测试", kind: "test_code", module: module };
      if (id.includes("_merge_code")) return { label: "架构师合并代码", kind: "merge_code", module: "" };
      if (id.includes("_global_test_data")) return { label: "架构师写全局测试数据", kind: "global_test_data", module: "" };
      if (id.includes("_global_test_code")) return { label: "架构师全局测试", kind: "global_test_code", module: "" };
      if (id.includes("_debug")) return { label: "代码修复 / debug", kind: "debug_code", module: module };
      return { label: id || "未知任务", kind: "unknown", module: module };
    }

    function showDetail(title, data) {
      const box = document.getElementById("detail");
      box.innerHTML = "";
      box.appendChild(el("h3", "", title));
      if (data.details_json) {
        box.appendChild(el("div", "muted", "details_json"));
        box.appendChild(el("pre", "", prettyDetails(data.details_json)));
      }
      box.appendChild(el("div", "muted", "raw"));
      box.appendChild(el("pre", "", JSON.stringify(data, null, 2)));
    }

    function prettyDetails(value) {
      try { return JSON.stringify(JSON.parse(value), null, 2); }
      catch (_) { return value; }
    }

    function parseDetails(value) {
      try { return JSON.parse(value || "{}"); }
      catch (_) { return {}; }
    }

    function resultChip(result) {
      const cls = result === "kok" ? "ok" : (result === "kbug" ? "warn" : (result ? "bad" : ""));
      return chip(result || "-", cls);
    }

    function chip(value, cls) {
      return el("span", "chip " + (cls || ""), value);
    }

    function row(label, value, title) {
      const wrap = el("div", "row");
      wrap.appendChild(el("span", "muted", label));
      const valueNode = el("span", "mono", value || "-");
      if (title) valueNode.title = title;
      wrap.appendChild(valueNode);
      return wrap;
    }

    function td(value, cls, title) {
      const cell = document.createElement("td");
      cell.className = cls || "";
      cell.textContent = value || "";
      if (title) cell.title = title;
      return cell;
    }

    function tdNode(node) {
      const cell = document.createElement("td");
      cell.appendChild(node);
      return cell;
    }

    function el(tag, cls, value) {
      const node = document.createElement(tag);
      if (cls) node.className = cls;
      if (value !== undefined) node.textContent = value;
      return node;
    }

    function text(id, value) {
      document.getElementById(id).textContent = String(value);
    }

    function shortID(value) {
      value = String(value || "");
      if (value === "-" || value.length <= 18) return value;
      const colon = value.indexOf(":");
      if (colon >= 0 && colon + 9 < value.length) {
        return value.slice(0, colon + 9) + "...";
      }
      return value.slice(0, 14) + "...";
    }

    function formatTime(value) {
      if (!value) return "-";
      const date = new Date(value);
      if (Number.isNaN(date.getTime())) return value;
      return date.toLocaleString();
    }

    function showError(message) {
      const box = document.getElementById("error");
      box.innerHTML = "";
      if (message) box.appendChild(el("div", "error", message));
    }

    function escapeHTML(value) {
      return String(value || "").replace(/[&<>"']/g, function (ch) {
        return ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[ch];
      });
    }

    function escapeAttr(value) {
      return escapeHTML(value);
    }

    load();
    syncAutoRefresh();
  </script>
</body>
</html>
`
