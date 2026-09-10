"use strict";

// Copy to Clipboard buttons with live status announcements
for (const button of document.querySelectorAll("[data-copy]")) {
  button.hidden = false;
  let timer;
  button.addEventListener("click", async () => {
    const feedback = document.getElementById(button.dataset.feedback);
    const code = document.getElementById(button.dataset.copy);
    if (!code) return;
    const text = code.textContent.trim().replace(/\s+/g, " ");

    try {
      await navigator.clipboard.writeText(text);
      button.textContent = "Copied";
      if (feedback) {
        feedback.textContent = "Command copied. Paste it into your terminal.";
      }
    } catch {
      const range = document.createRange();
      range.selectNodeContents(code);
      const selection = window.getSelection();
      selection.removeAllRanges();
      selection.addRange(range);
      if (feedback) {
        feedback.textContent = "Select and copy the command with your browser.";
      }
    }

    clearTimeout(timer);
    timer = setTimeout(() => {
      button.textContent = "Copy";
      if (feedback) {
        feedback.textContent = "";
      }
    }, 4000);
  });
}

// Interactive Desktop Demo Tabs (Keyboard accessible: Left, Right, Home, End)
const tabs = [...document.querySelectorAll("[data-demo-tab]")];

function selectTab(tab) {
  for (const item of tabs) {
    const selected = item === tab;
    item.setAttribute("aria-selected", String(selected));
    item.tabIndex = selected ? 0 : -1;
    const targetId = item.getAttribute("aria-controls");
    const targetPanel = document.getElementById(targetId);
    if (targetPanel) {
      targetPanel.hidden = !selected;
    }
  }
}

for (const tab of tabs) {
  tab.addEventListener("click", () => selectTab(tab));
  tab.addEventListener("keydown", (event) => {
    const index = tabs.indexOf(tab);
    let next;
    if (event.key === "ArrowRight") next = tabs[(index + 1) % tabs.length];
    if (event.key === "ArrowLeft") next = tabs[(index + tabs.length - 1) % tabs.length];
    if (event.key === "Home") next = tabs[0];
    if (event.key === "End") next = tabs[tabs.length - 1];
    if (next) {
      event.preventDefault();
      selectTab(next);
      next.focus();
    }
  });
}

const demoTabs = document.querySelector("[data-demo-tabs]");
if (demoTabs) {
  demoTabs.hidden = false;
}
