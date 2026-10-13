import { expect, test } from "@playwright/test";
import type { Page } from "@playwright/test";

async function login(page: Page, username: string) {
  await page.goto("/");
  await page.getByLabel("用户名", { exact: true }).fill(username);
  await page.getByLabel("密码", { exact: true }).fill("e2e-test-password-123");
  await page.getByRole("button", { name: "登录工作台" }).click();
  await expect(page.getByRole("button", { name: "＋ 新建会话" })).toBeVisible();
}

async function createConversation(page: Page, title: string) {
  await page.getByRole("button", { name: "＋ 新建会话" }).click();
  await page.getByRole("button", { name: "修改标题" }).click();
  await page
    .getByRole("textbox", { name: "会话标题", exact: true })
    .fill(title);
  await page.getByRole("button", { name: "保存", exact: true }).click();
  await expect(
    page.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
}

test("login, save, refresh, switch, rename, logout and user isolation", async ({
  page,
  browser,
}) => {
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await login(page, "e2e_alice");
  const title = `会话验收-${Date.now()}`;
  await createConversation(page, title);
  const text = "这个月的客户使用情况怎么样？\n先记录问题 👋";
  await page.getByRole("textbox", { name: "消息内容" }).fill(text);
  await page.getByRole("button", { name: "发送消息" }).click();
  await expect(page.getByText("已保存", { exact: true }).first()).toBeVisible();
  await page.reload();
  await expect(page.getByText(text, { exact: true })).toBeVisible();
  await expect(
    page.getByRole("heading", { name: title, exact: true }),
  ).toBeVisible();
  await createConversation(page, `另一个会话-${Date.now()}`);
  await expect(page.getByText(text, { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: new RegExp(title) }).click();
  await expect(page.getByText(text, { exact: true })).toBeVisible();

  const otherContext = await browser.newContext();
  const other = await otherContext.newPage();
  await login(other, "e2e_bob");
  await expect(
    other.getByRole("button", { name: new RegExp(title) }),
  ).toHaveCount(0);
  await otherContext.close();

  await page.screenshot({
    path: "test-results/chat-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(page.getByRole("textbox", { name: "消息内容" })).toBeVisible();
  const horizontalOverflow = await page.evaluate(
    () => document.documentElement.scrollWidth > window.innerWidth,
  );
  expect(horizontalOverflow).toBe(false);
  await page.screenshot({
    path: "test-results/chat-mobile.png",
    fullPage: true,
  });
  await page.getByRole("button", { name: "退出", exact: true }).click();
  await expect(page.getByRole("button", { name: "登录工作台" })).toBeVisible();
  await login(page, "e2e_alice");
  await expect(page.getByText(text, { exact: true })).toBeVisible();
  expect(errors).toEqual([]);
});

test("lost response retries with the same request ID and saves one message", async ({
  page,
}) => {
  await login(page, "e2e_alice");
  await createConversation(page, `网络重试-${Date.now()}`);
  let dropped = false;
  const ids: string[] = [];
  await page.route("**/api/conversations/*/messages", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    const input = route.request().postDataJSON() as { request_id: string };
    ids.push(input.request_id);
    if (!dropped) {
      dropped = true;
      await route.fetch(); // The database commits, but the browser never receives the response.
      await route.abort("failed");
    } else await route.continue();
  });
  const text = `请勿重复保存-${Date.now()}`;
  await page.getByRole("textbox", { name: "消息内容" }).fill(text);
  await page.getByRole("button", { name: "发送消息" }).click();
  await page.getByRole("button", { name: "重试保存" }).click();
  await expect(page.getByText("已保存", { exact: true }).first()).toBeVisible();
  expect(ids).toHaveLength(2);
  expect(ids[0]).toBe(ids[1]);
  await page.reload();
  await expect(page.getByText(text, { exact: true })).toHaveCount(1);
});

test("AI clarification, explicit confirmation, amendment and generation retry", async ({
  page,
}) => {
  await login(page, "e2e_alice");
  await createConversation(page, `需求澄清-${Date.now()}`);
  await page.getByRole("textbox", { name: "消息内容" }).fill("分析客户留存");
  await page.getByRole("button", { name: "发送消息" }).click();
  await expect(
    page.getByText("请补充分析的时间范围和留存口径。", { exact: true }),
  ).toBeVisible({ timeout: 15000 });
  await page.reload();
  await expect(page.getByText("需求摘要", { exact: true })).toBeVisible();
  await expect(
    page.getByRole("button", { name: "确认需求", exact: true }),
  ).toHaveCount(0);

  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill("2026年9月，按北京时间统计付费用户的次月留存，给出表格");
  await page.getByRole("button", { name: "发送消息" }).click();
  await page.getByRole("button", { name: "确认需求", exact: true }).click();
  await expect(page.getByText("已确认", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText("已确认", { exact: true })).toBeVisible();

  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill("测试生成失败：改成8月");
  await page.getByRole("button", { name: "发送消息" }).click();
  await expect(page.getByRole("button", { name: "重试生成" })).toBeVisible({
    timeout: 15000,
  });
  await page.reload();
  await page.getByRole("button", { name: "重试生成" }).click();
  await expect(page.getByRole("button", { name: "重试生成" })).toHaveCount(0);
  await expect(page.getByText("澄清中", { exact: true })).toBeVisible();
  await expect(
    page.getByText("请补充分析的时间范围和留存口径。", { exact: true }),
  ).toHaveCount(2, { timeout: 15000 });
  await expect(
    page.getByText("测试生成失败：改成8月", { exact: true }),
  ).toHaveCount(1);
});
