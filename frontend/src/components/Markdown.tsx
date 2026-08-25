import { marked } from "marked";
import DOMPurify from "dompurify";
import { memo } from "react";

marked.setOptions({ gfm: true, breaks: true });

function render(text: string): string {
  const html = marked.parse(text, { async: false }) as string;
  return DOMPurify.sanitize(html, {
    FORBID_TAGS: ["style", "iframe", "form", "input"],
    FORBID_ATTR: ["onerror", "onclick", "onload"],
  });
}

const Markdown = memo(function Markdown({ text }: { text: string }) {
  return <div className="md" dangerouslySetInnerHTML={{ __html: render(text) }} />;
});

export default Markdown;
