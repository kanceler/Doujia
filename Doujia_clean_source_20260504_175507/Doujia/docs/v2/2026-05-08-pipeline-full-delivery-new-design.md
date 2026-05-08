# Pipeline Full Delivery New Design

> Date: 2026-05-08
>
> Status: draft for review. This document describes the recommended new
> semantic shape for `pipeline_full_delivery.spec.json`; it is not yet the
> final JSON implementation.

---

## 1. Design Goal

The new full-delivery pipeline should be friendly to the run/snapshot/ref
scheduler:

- every fanout creates concrete branch work
- every merge waits for a complete compatible scene
- every child pipeline return is explicit
- every repair preserves failed history and creates a new branch
- task remains the execution unit
- DoujiaGit facts remain the scheduler truth

The important principle is that the JSON should describe legal runtime scenes,
not just a list of task statuses.

---

## 2. Recommended Top-Level Shape

`pipeline_full_delivery` should stay as the entry pipeline, but it should not
manage every module branch directly. It should coordinate high-level delivery
milestones.

Recommended flow:

```text
pipeline_full_delivery
  [ceo_write_requirement]
  [pm_write_product_plan]
  [ceo_review_product_plan]
  [architect_write_architecture]
  [pm_review_architecture]
  [architect_create_container]
  [architect_split_modules]
  fanout:
    call pipeline_front_module
    call pipeline_backend_module_group
    call pipeline_global_test_data
  merge:
    front_module_return
    backend_group_return
    global_test_data_return
  [architect_merge_code]
  [architect_global_test_code]
  delivery_done
```

Top-level frontier intent:

```text
{split_done}
- {split_done}
+ {front_running, backend_group_running, global_test_data_running}
= {front_running, backend_group_running, global_test_data_running}
```

Then:

```text
{front_done, backend_group_done, global_test_data_done}
- {front_done, backend_group_done, global_test_data_done}
+ {merge_ready}
= {merge_ready}
```

This keeps the top-level frontier readable and avoids leaking every backend
module branch into the entry pipeline.

---

## 3. Module Pipeline

Ordinary module development should be a reusable subpipeline:

```text
pipeline_module
  start: {module_input_ready}
  fanout:
    call pipeline_write_code
    call pipeline_write_test_data
  merge:
    {code_returned, test_data_returned}
  call pipeline_test_code
  delivery: {module_tested}
```

The merge is bag-based:

```text
join_bags by module_key:
  code_bag
  test_data_bag
```

Important semantics:

- `code_bag` and `test_data_bag` may arrive in either order.
- The first arrival checks merge readiness and waits.
- The second arrival re-checks merge readiness.
- Only the complete compatible scene starts `pipeline_test_code`.
- Partial merge is normal waiting, not failure.

---

## 4. Code and Test-Data Subpipelines

`pipeline_write_code`:

```text
input:
  module_input

task:
  [write_code]

output:
  code_bag indexed by module_key and coder_alias
```

`pipeline_write_test_data`:

```text
input:
  module_input

task:
  [write_test_data]

output:
  test_data_bag indexed by module_key and tester_alias
```

Each subpipeline must declare its own agents in `namespace.agents` or
`signature.agents`. Parent pipelines should pass agent bindings explicitly.

---

## 5. Test-Code and Repair Pipeline

`pipeline_test_code` is the core repair-aware subpipeline.

Input:

```text
module_input
code_bag
test_data_bag
```

Main task:

```text
[test_code]
```

Success:

```text
kok -> tested_module
```

Bug:

```text
kbug:
  preserve failed test snapshot
  default to full module repair
```

Default repair scope:

```text
{test_failed_v1}
-> repair target scene:
   {module_input_ready_v1}
-> rerun in parallel:
   [write_code]
   [write_test_data]
-> {code_returned_v2, test_data_returned_v2}
-> retry [test_code]
```

Reason:

```text
When `test_code` returns `kbug`, the system often cannot know whether the bug
comes from implementation code, test data, or the test script itself. The
default recover scope should therefore return to the smallest complete module
input scene and regenerate both code and test data.
```

The failed snapshot remains immutable history. The new attempt is a parallel
branch/version, not a rewrite of the old attempt.

Optional partial repair:

```text
If a pipeline or handler explicitly says the test data is reusable:

{test_failed_v1}
-> repair target scene:
   {write_code_precondition_v1, test_data_returned_v1}
-> repaired code v2
-> {code_returned_v2, test_data_returned_v1}
-> retry [test_code]
```

Partial repair is allowed, but it must be opt-in. The default should not silently
reuse `test_data_bag`.

Required metadata:

- failed snapshot remains immutable
- failure report bag is recorded
- previous code bag is recorded
- previous test-data bag is recorded
- full repair target scene is recorded
- reusable test-data snapshot is recorded only for explicit partial repair
- repair target task/transition is recorded

---

## 6. Frontend Module Pipeline

The frontend module extends ordinary module development with preview review:

```text
pipeline_front_module
  run code + test_data + test_code repair loop
  call pipeline_front_preview_review
  delivery: front_module_approved
```

Preview review loop:

```text
[preview_edit]
[user_preview_confirm]

kok:
  return approved preview

krewrite:
  keep module_input
  keep tested_module
  keep code_bag
  add preview_feedback_bag
  retry preview_edit

kfail:
  terminal failure
```

`krewrite` is a localized preview loop. It should not restart code writing or
module testing unless the pipeline explicitly asks for that.

---

## 7. Backend Module Group Pipeline

The new design should introduce `pipeline_backend_module_group`.

Purpose:

```text
pipeline_backend_module_group
  input:
    backend module_input collection

  fanout:
    call pipeline_module for each backend module_input

  merge:
    wait for all backend module returns

  delivery:
    tested_module collection
    code_bag collection
```

Reason:

- keeps top-level `pipeline_full_delivery` small
- isolates backend fanout/fanin semantics
- makes collection repair easier to reason about later
- avoids top-level direct ownership of every backend module branch

---

## 8. Global Test Data Pipeline

`pipeline_global_test_data` remains simple:

```text
input:
  global_test_input

task:
  [write_global_test_data]

output:
  global_test_data
```

This runs in parallel with frontend and backend module work after split.

---

## 9. Merge Code Pipeline

`pipeline_merge_code` should merge completed module outputs:

```text
input:
  front tested_module/code_bag
  backend tested_module/code_bag collection
  container_context
  global_test_input

task:
  [merge_code]

output:
  merged_code
```

The top-level merge before this task should wait for:

```text
front_module_done
backend_group_done
global_test_data_done
```

The merge task itself should not be dispatched until all required return facts
exist.

---

## 10. Global Test and Repair

`pipeline_global_test_code` handles final integration test and repair.

Input:

```text
merged_code
global_test_data
container_context if needed
```

Main task:

```text
[global_test_code]
```

Success:

```text
kok -> global_test_report -> delivery_done
```

Bug:

```text
kbug:
  preserve failed global test snapshot
  keep global_test_data
  keep container_context if needed
  include failure_report
  run debug_global_code
  retry global_test_code
```

The repair target is the smallest legal global test scene, not a blind rollback
to a single snapshot.

---

## 11. JSON Authoring Rules

The new JSON should follow these rules:

1. Every pipeline declares the agent aliases it uses.
2. Every `call` passes `params`, `input_bags`, and `agent_bindings`
   explicitly.
3. Repair/debug uses `handlers`, `exported_handlers`, and
   `exception_handlers` rather than only old `next.by_result.ref` fields.
4. Bag fanin prefers `aggregate.mode = "join_bags"` when concrete bag matching
   is required.
5. State fanin uses `aggregate.mode = "all"` only when state completion alone
   is truly enough.
6. Top-level module collections should be wrapped in a group pipeline.
7. Module test `kbug` defaults to full module repair:
   - rerun code generation
   - rerun test-data generation
   - preserve the failed test snapshot
   - pass failure report and previous attempt context into the new attempt where
     useful
8. Partial repair is opt-in and must declare which sibling branches are reusable.
9. Repair handlers must declare:
   - replaced bags
   - reusable bags
   - failure report input
   - retry target
   - owner instance

---

## 12. Recommended New Pipeline List

Recommended registry definitions:

```text
pipeline_full_delivery
pipeline_write_code
pipeline_write_test_data
pipeline_test_code
pipeline_module
pipeline_backend_module_group
pipeline_front_write_code
pipeline_front_test_code
pipeline_front_preview_review
pipeline_front_module
pipeline_global_test_data
pipeline_merge_code
pipeline_global_test_code
```

Optional later split:

```text
pipeline_review_plan
pipeline_review_architecture
pipeline_user_preview_confirm
```

These optional splits are not necessary for the first new full-delivery JSON.

---

## 13. Review Questions

Please review these before we write the JSON:

- Is top-level three-way fanout correct:
  `front module`, `backend module group`, `global test data`?
- Should backend modules be wrapped in `pipeline_backend_module_group`?
- Should frontend preview rewrite stay local to preview instead of restarting
  module code/test?
- Should module `kbug` default to full module repair: rerun both code and test
  data, with partial code-only repair allowed only by explicit handler policy?
- Should global `kbug` repair merged/global code while reusing global test data?
- Do we want state-level `mode="all"` anywhere, or should the first version use
  mostly `join_bags`?
