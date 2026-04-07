document.addEventListener("DOMContentLoaded", () => {
  const headings = document.querySelectorAll(".content h2[id], .content h3[id], .content h4[id]");

  for (const heading of headings) {
    if (heading.querySelector(".heading-anchor")) {
      continue;
    }

    const anchor = document.createElement("a");
    anchor.className = "heading-anchor";
    anchor.href = `#${heading.id}`;
    anchor.setAttribute("aria-label", `Link to section: ${heading.textContent.trim()}`);
    anchor.innerHTML = `
      <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" width="15" height="15" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <path d="M10 13a5 5 0 0 0 7.07 0l3.54-3.54a5 5 0 0 0-7.07-7.07L11 4"></path>
        <path d="M14 11a5 5 0 0 0-7.07 0L3.39 14.54a5 5 0 1 0 7.07 7.07L13 20"></path>
      </svg>
    `;

    heading.appendChild(anchor);
  }
});
