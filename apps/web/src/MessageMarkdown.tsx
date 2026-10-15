import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import styles from "./MessageMarkdown.module.css";

export default function MessageMarkdown({ content }: { content: string }) {
  return (
    <div className={styles.markdown}>
      <Markdown
        remarkPlugins={[remarkGfm]}
        skipHtml
        components={{
          a: ({ href, children }) => (
            <a href={href} target="_blank" rel="noopener noreferrer">
              {children}
            </a>
          ),
          table: ({ children }) => (
            <div
              className={styles.tableScroll}
              tabIndex={0}
              aria-label="Markdown 表格"
            >
              <table>{children}</table>
            </div>
          ),
          img: ({ alt }) => <span>{alt ? `[图片：${alt}]` : "[图片]"}</span>,
        }}
      >
        {content}
      </Markdown>
    </div>
  );
}
