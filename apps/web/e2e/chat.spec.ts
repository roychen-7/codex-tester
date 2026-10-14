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
  test.skip(
    process.env.E2E_AGENT_MODE !== "legacy",
    "Legacy workflow regression",
  );
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
    page.getByRole("button", { name: "确认并分析", exact: true }),
  ).toHaveCount(0);

  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill("2026年9月，按北京时间统计付费用户的次月留存，给出表格");
  await page.getByRole("button", { name: "发送消息" }).click();
  await page.getByRole("button", { name: "确认并分析", exact: true }).click();
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

test("confirmed analysis calls MCP and persists SQL and result table", async ({
  page,
}) => {
  test.skip(
    process.env.E2E_AGENT_MODE !== "legacy",
    "Legacy workflow regression",
  );
  await login(page, "e2e_alice");
  await createConversation(page, `模拟分析-${Date.now()}`);
  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill(
      "2026年9月，UTC，按天统计所有客户的已完成 balance 充值 pay_amount，不扣退款，给出表格",
    );
  await page.getByRole("button", { name: "发送消息" }).click();
  await page.getByRole("button", { name: "确认并分析", exact: true }).click();
  await expect(
    page.getByText("模拟数据：9月1日充值150，9月2日充值20，合计170。", {
      exact: true,
    }),
  ).toBeVisible({ timeout: 20000 });
  await expect(page.getByRole("table")).toBeVisible();
  await page.getByText("查看 SQL 与数据范围", { exact: true }).click();
  await expect(page.getByText(/SELECT date_trunc/)).toBeVisible();
  await page.reload();
  await expect(page.getByRole("table")).toBeVisible();
  await expect(
    page.getByText("模拟数据：9月1日充值150，9月2日充值20，合计170。", {
      exact: true,
    }),
  ).toHaveCount(1);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth,
    ),
  ).toBe(false);
  await page.getByRole("table").scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "test-results/analysis-mobile.png",
    fullPage: true,
  });
});

test("analysis failure can be retried without duplicate answer", async ({
  page,
}) => {
  test.skip(
    process.env.E2E_AGENT_MODE !== "legacy",
    "Legacy workflow regression",
  );
  await login(page, "e2e_alice");
  await createConversation(page, `分析重试-${Date.now()}`);
  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill(`2026年9月充值分析 analysis-failure-once ${Date.now()}`);
  await page.getByRole("button", { name: "发送消息" }).click();
  await page.getByRole("button", { name: "确认并分析", exact: true }).click();
  await page.getByRole("button", { name: "重试分析", exact: true }).click();
  await expect(page.getByRole("table")).toBeVisible({ timeout: 20000 });
  await expect(
    page.getByText("模拟数据：9月1日充值150，9月2日充值20，合计170。", {
      exact: true,
    }),
  ).toHaveCount(1);
});

test("cancel analysis stops the worker from saving an answer", async ({
  page,
}) => {
  test.skip(
    process.env.E2E_AGENT_MODE !== "legacy",
    "Legacy workflow regression",
  );
  await login(page, "e2e_alice");
  await createConversation(page, `取消分析-${Date.now()}`);
  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill("2026年9月充值分析 analysis-slow");
  await page.getByRole("button", { name: "发送消息" }).click();
  await page.getByRole("button", { name: "确认并分析", exact: true }).click();
  await page.getByRole("button", { name: "取消分析", exact: true }).click();
  await expect(page.getByText("分析已取消", { exact: true })).toBeVisible();
  // The mock provider completes after three seconds; the cancelled attempt must stay terminal.
  await page.waitForTimeout(3500);
  await page.reload();
  await expect(page.getByText("分析已取消", { exact: true })).toBeVisible();
  await expect(page.getByRole("table")).toHaveCount(0);
  await expect(
    page.getByText("模拟数据：9月1日充值150，9月2日充值20，合计170。", {
      exact: true,
    }),
  ).toHaveCount(0);
});

test("continuous conversation answers directly and resumes after clarification", async ({
  page,
}) => {
  test.skip(
    process.env.E2E_AGENT_MODE === "legacy",
    "New conversation runtime",
  );
  await login(page, "e2e_alice");
  await createConversation(page, `连续对话-${Date.now()}`);
  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill("留存和转化有什么区别？");
  await page.getByRole("button", { name: "发送消息" }).click();
  await expect(
    page.getByText(
      "留存关注用户是否持续回来使用，转化关注用户是否完成某个目标行为。",
      { exact: true },
    ),
  ).toBeVisible({ timeout: 15000 });
  await expect(page.getByText("需求摘要", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "确认并分析" })).toHaveCount(0);
  await page.getByRole("textbox", { name: "消息内容" }).fill("我想优化网站");
  await page.getByRole("button", { name: "发送消息" }).click();
  await expect(
    page.getByText("你目前更关心提高留存，还是增加收入？", { exact: true }),
  ).toBeVisible({ timeout: 15000 });
  await page.reload();
  await expect(
    page.getByText("等待你的补充，可以直接在下方回复。", { exact: true }),
  ).toBeVisible();
  await page.getByRole("textbox", { name: "消息内容" }).fill("优先留存");
  await page.getByRole("button", { name: "发送消息" }).click();
  await expect(
    page.getByText(
      "结合你之前的网站优化目标，我们先讨论留存：可以从新用户首次成功使用的体验入手。",
      { exact: true },
    ),
  ).toBeVisible({ timeout: 15000 });
  await page.reload();
  await expect(
    page.getByText(
      "结合你之前的网站优化目标，我们先讨论留存：可以从新用户首次成功使用的体验入手。",
      { exact: true },
    ),
  ).toHaveCount(1);
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth,
    ),
  ).toBe(false);
});

test("continuous conversation retries and cancels an in-flight reply", async ({
  page,
}) => {
  test.skip(
    process.env.E2E_AGENT_MODE === "legacy",
    "New conversation runtime",
  );
  await login(page, "e2e_alice");
  await createConversation(page, `对话重试-${Date.now()}`);
  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill("dialogue-failure-once");
  await page.getByRole("button", { name: "发送消息" }).click();
  await page.getByRole("button", { name: "重试回复", exact: true }).click();
  await expect(
    page.getByText(
      "留存关注用户是否持续回来使用，转化关注用户是否完成某个目标行为。",
      { exact: true },
    ),
  ).toBeVisible({ timeout: 15000 });
  await page.getByRole("textbox", { name: "消息内容" }).fill("dialogue-slow");
  await page.getByRole("button", { name: "发送消息" }).click();
  await page.getByRole("button", { name: "停止回复", exact: true }).click();
  await expect(page.getByText("回复已停止", { exact: true })).toBeVisible();
  await page.waitForTimeout(3500);
  await page.reload();
  await expect(page.getByText("回复已停止", { exact: true })).toBeVisible();
  await expect(
    page.getByText(
      "留存关注用户是否持续回来使用，转化关注用户是否完成某个目标行为。",
      { exact: true },
    ),
  ).toHaveCount(1);
});

test("new input supersedes an in-flight conversation reply", async ({
  page,
}) => {
  test.skip(
    process.env.E2E_AGENT_MODE === "legacy",
    "New conversation runtime",
  );
  await login(page, "e2e_alice");
  await createConversation(page, `中途补充-${Date.now()}`);
  await page.getByRole("textbox", { name: "消息内容" }).fill("dialogue-slow");
  await page.getByRole("button", { name: "发送消息" }).click();
  await expect(
    page.getByRole("button", { name: "停止回复", exact: true }),
  ).toBeVisible();
  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill("改一下，我想优化网站");
  await page.getByRole("button", { name: "发送消息" }).click();
  await expect(
    page.getByText("你目前更关心提高留存，还是增加收入？", { exact: true }),
  ).toBeVisible({ timeout: 15000 });
  await page.reload();
  await expect(
    page.getByText("你目前更关心提高留存，还是增加收入？", { exact: true }),
  ).toHaveCount(1);
  await expect(
    page.getByText(
      "留存关注用户是否持续回来使用，转化关注用户是否完成某个目标行为。",
      { exact: true },
    ),
  ).toHaveCount(0);
});

for (const askAfterQuery of [false, true]) {
  test(`conversation queries without confirmation and reuses evidence (${askAfterQuery ? "asks after query" : "direct answer"})`, async ({
    page,
  }) => {
    test.skip(
      process.env.E2E_AGENT_MODE === "legacy",
      "New conversation runtime",
    );
    await login(page, "e2e_alice");
    await createConversation(page, `连续查询-${Date.now()}`);
    await page
      .getByRole("textbox", { name: "消息内容" })
      .fill(
        `UTC 2026年9月，全部客户，COMPLETED balance 充值，pay_amount，不扣退款，按天金额和订单数。conversation-query ${askAfterQuery ? "query-then-ask" : ""}`,
      );
    await page.getByRole("button", { name: "发送消息" }).click();
    await expect(page.getByRole("table")).toBeVisible({ timeout: 20000 });
    await expect(
      page.getByRole("button", { name: "确认并分析", exact: true }),
    ).toHaveCount(0);
    const firstReply = page
      .locator("article")
      .filter({ has: page.getByRole("table") });
    await expect(firstReply).toContainText("合计170");
    await expect(
      firstReply.getByRole("cell", { name: "150.00", exact: true }),
    ).toBeVisible();
    await firstReply.getByText("查看 SQL 与数据范围", { exact: true }).click();
    await expect(firstReply).toContainText("analytics.payment_orders");
    if (askAfterQuery) {
      await expect(
        page.getByText("等待你的补充，可以直接在下方回复。", { exact: true }),
      ).toBeVisible();
    }
    await page.reload();
    await expect(page.getByRole("table")).toHaveCount(1);
    await page
      .getByRole("textbox", { name: "消息内容" })
      .fill("沿用刚才的数据，按订单算平均每笔充值金额，不用重新查询。");
    await page.getByRole("button", { name: "发送消息" }).click();
    await expect(
      page.getByText(
        "沿用刚才的模拟查询：合计170，共3笔充值，平均每笔约56.67。",
        { exact: true },
      ),
    ).toBeVisible({ timeout: 20000 });
    await expect(page.getByRole("table")).toHaveCount(2);
    await page.reload();
    await expect(page.getByRole("table")).toHaveCount(2);
    await expect(
      page.locator("article").filter({ has: page.getByRole("table") }),
    ).toHaveCount(2);
    await page.setViewportSize({ width: 390, height: 844 });
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth > window.innerWidth,
      ),
    ).toBe(false);
  });
}

test("chat separates roles and renders safe Markdown on desktop and mobile", async ({
  page,
}) => {
  test.skip(process.env.E2E_AGENT_MODE === "legacy", "Conversation rendering");
  const errors: string[] = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await login(page, "e2e_alice");
  await createConversation(page, `Markdown-${Date.now()}`);
  await page
    .getByRole("textbox", { name: "消息内容" })
    .fill("Markdown 展示测试 **保留我的原文**");
  await page.getByRole("button", { name: "发送消息" }).click();
  const user = page.locator('article[data-role="user"]');
  const agent = page.locator('article[data-role="assistant"]');
  await expect(agent.getByRole("heading", { name: "充值分析" })).toBeVisible({
    timeout: 15000,
  });
  await expect(user).toContainText("**保留我的原文**");
  await expect(
    agent.locator("strong").filter({ hasText: "170.00" }),
  ).toBeVisible();
  await expect(agent.getByRole("listitem")).toHaveCount(2);
  await expect(agent.getByRole("table")).toBeVisible();
  await expect(agent.locator("pre code")).toContainText(
    "SELECT SUM(pay_amount)",
  );
  await expect(agent.locator("blockquote")).toContainText("模拟数据");
  await expect(agent.locator("img, script")).toHaveCount(0);
  await expect(agent.locator('a[href^="javascript:"]')).toHaveCount(0);
  await expect(agent.getByRole("link", { name: "说明链接" })).toHaveAttribute(
    "rel",
    "noopener noreferrer",
  );

  const userBox = await user.boundingBox();
  const agentBox = await agent.boundingBox();
  expect(userBox!.x).toBeGreaterThan(agentBox!.x);
  const userColor = await user
    .locator(":scope > div")
    .nth(1)
    .evaluate((el) => getComputedStyle(el).backgroundColor);
  const agentColor = await agent
    .locator(":scope > div")
    .nth(1)
    .evaluate((el) => getComputedStyle(el).backgroundColor);
  expect(userColor).not.toBe(agentColor);
  await agent.scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "test-results/chat-markdown-desktop.png",
    fullPage: true,
  });
  await page.setViewportSize({ width: 390, height: 844 });
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth > window.innerWidth,
    ),
  ).toBe(false);
  await agent.scrollIntoViewIfNeeded();
  await page.screenshot({
    path: "test-results/chat-markdown-mobile.png",
    fullPage: true,
  });
  await page.reload();
  await expect(agent.getByRole("heading", { name: "充值分析" })).toBeVisible();
  expect(errors).toEqual([]);
});
