# Proposed website section: Plan and build from a brief

Not applied. Drop into `site/index.html` after the "Why this one" section (before the `<hr class="rule">` that precedes `#install`) once the csvstat smoke passes. Copy is 58 words and claims only what README.md section "Plan and build from a brief" says.

```html
  <section class="wrap" id="brief">
    <h2>Plan and build from a brief</h2>
    <div class="claims">

      <div class="claim">
        <h3>One brief, one run</h3>
        <p><code>/project &lt;brief&gt;</code> plans a brief into a tree of nodes,
        then builds it. Independent leaves build in parallel, and a leaf that
        runs out of models is rewritten by the planner.</p>
        <p class="proof">Every node, event and attempt is a plain file in
        <code>.gophermind/</code> in your repo.</p>
      </div>

    </div>
  </section>

  <hr class="rule">
```
