/* 视联 AI 运维平台 · 领导汇报图表初始化 */
(function () {
  'use strict';

  var root = document.documentElement;

  function css(name, fallback) {
    var value = getComputedStyle(root).getPropertyValue(name);
    return value && value.trim() ? value.trim() : fallback;
  }

  var C = {
    bg: css('--bg', '#F4F6FA'),
    bg2: css('--bg2', '#FFFFFF'),
    ink: css('--ink', '#0F1B2D'),
    muted: css('--muted', '#5B6B82'),
    rule: css('--rule', '#D8E0EA'),
    accent: css('--accent', '#1E5AA8'),
    accentSoft: css('--accent-soft', '#E8F0FA'),
    accentBorder: css('--accent-border', '#C5D8EF'),
    accent2: css('--accent2', '#0E8A7B'),
    amber: css('--amber', '#B87A22')
  };

  var FONT = '"Instrument Sans","PingFang SC","Noto Sans SC","Microsoft YaHei",sans-serif';

  var charts = [];

  function mount(id, option) {
    var el = document.getElementById(id);
    if (!el || !window.echarts) return;
    var chart = window.echarts.init(el, null, { renderer: 'canvas' });
    chart.setOption(option);
    charts.push(chart);
  }

  function axisText(size) {
    return { color: C.muted, fontSize: size || 11, fontFamily: FONT };
  }

  /* 1. 五步法接口覆盖：目标引用 vs 实际使用 */
  mount('chart-coverage', {
    animationDuration: 700,
    grid: { left: 6, right: 34, top: 34, bottom: 6, containLabel: true },
    legend: {
      data: ['目标引用', '平台已接入', '排障链路已编排'],
      top: 0,
      right: 0,
      itemWidth: 10,
      itemHeight: 10,
      itemGap: 16,
      textStyle: { color: C.muted, fontSize: 12, fontFamily: FONT }
    },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: C.bg2,
      borderColor: C.rule,
      textStyle: { color: C.ink, fontSize: 12, fontFamily: FONT },
      formatter: function (params) {
        var lines = [params[0].name];
        params.forEach(function (p) {
          lines.push(p.marker + p.seriesName + '：' + p.value + ' 个接口');
        });
        return lines.join('<br/>');
      }
    },
    xAxis: {
      type: 'value',
      minInterval: 1,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: axisText(11),
      splitLine: { lineStyle: { color: C.rule, type: 'dashed' } }
    },
    yAxis: {
      type: 'category',
      inverse: true,
      data: ['1 发现', '2 确认与定界', '3 定位', '4 取证', '5 定因'],
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: C.ink, fontSize: 12, fontFamily: FONT }
    },
    series: [
      {
        name: '目标引用',
        type: 'bar',
        barWidth: 8,
        barGap: '30%',
        data: [6, 12, 9, 10, 11],
        itemStyle: {
          color: C.accentSoft,
          borderColor: C.accentBorder,
          borderWidth: 1,
          borderRadius: 4
        },
        label: {
          show: true,
          position: 'right',
          color: C.muted,
          fontSize: 11,
          fontFamily: FONT,
          formatter: '{c}'
        }
      },
      {
        name: '平台已接入',
        type: 'bar',
        barWidth: 7,
        data: [6, 6, 2, 6, 1],
        itemStyle: { color: C.accent2, opacity: 0.45, borderRadius: 4 },
        label: {
          show: true,
          position: 'right',
          color: C.muted,
          fontSize: 11,
          fontFamily: FONT,
          formatter: '{c}'
        }
      },
      {
        name: '排障链路已编排',
        type: 'bar',
        barWidth: 7,
        data: [2, 6, 2, 3, 1],
        itemStyle: { color: C.accent, borderRadius: 4 },
        label: {
          show: true,
          position: 'right',
          color: C.ink,
          fontSize: 11,
          fontWeight: 600,
          fontFamily: FONT,
          formatter: '{c}'
        }
      }
    ]
  });

  /* 4. 四源接口总量 vs 已编排 */
  mount('chart-sources', {
    animationDuration: 700,
    grid: { left: 6, right: 30, top: 42, bottom: 6, containLabel: true },
    legend: {
      data: ['接口总量', '平台已编排'],
      top: 0,
      right: 0,
      itemWidth: 10,
      itemHeight: 10,
      itemGap: 16,
      textStyle: { color: C.muted, fontSize: 12, fontFamily: FONT }
    },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: C.bg2,
      borderColor: C.rule,
      textStyle: { color: C.ink, fontSize: 12, fontFamily: FONT },
      formatter: function (params) {
        var lines = [params[0].name];
        params.forEach(function (p) {
          lines.push(p.marker + p.seriesName + '：' + p.value + ' 个');
        });
        return lines.join('<br/>');
      }
    },
    xAxis: {
      type: 'category',
      data: ['Pinpoint', 'Prometheus', 'CMDB', 'Elasticsearch'],
      axisLine: { lineStyle: { color: C.rule } },
      axisTick: { show: false },
      axisLabel: { color: C.ink, fontSize: 12, fontFamily: FONT }
    },
    yAxis: {
      type: 'value',
      minInterval: 1,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: axisText(11),
      splitLine: { lineStyle: { color: C.rule, type: 'dashed' } }
    },
    series: [
      {
        name: '接口总量',
        type: 'bar',
        barWidth: 22,
        barGap: '25%',
        data: [22, 36, 5, 5],
        itemStyle: {
          color: C.accentSoft,
          borderColor: C.accentBorder,
          borderWidth: 1,
          borderRadius: 4
        },
        label: {
          show: true,
          position: 'top',
          color: C.muted,
          fontSize: 11,
          fontFamily: FONT
        }
      },
      {
        name: '平台已编排',
        type: 'bar',
        barWidth: 22,
        data: [4, 7, 1, 3],
        itemStyle: { color: C.accent, borderRadius: 4 },
        label: {
          show: true,
          position: 'top',
          color: C.ink,
          fontSize: 11,
          fontWeight: 600,
          fontFamily: FONT
        }
      }
    ]
  });

  /* 2. 闭环成熟度评估 */
  mount('chart-loops', {
    animationDuration: 700,
    grid: { left: 6, right: 44, top: 10, bottom: 6, containLabel: true },
    tooltip: {
      trigger: 'axis',
      axisPointer: { type: 'shadow' },
      backgroundColor: C.bg2,
      borderColor: C.rule,
      textStyle: { color: C.ink, fontSize: 12, fontFamily: FONT },
      formatter: function (params) {
        return params[0].name + '<br/>' + params[0].marker + '成熟度：' + params[0].value + ' / 100';
      }
    },
    xAxis: {
      type: 'value',
      max: 100,
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: axisText(11),
      splitLine: { lineStyle: { color: C.rule, type: 'dashed' } }
    },
    yAxis: {
      type: 'category',
      inverse: true,
      data: [
        '告警 → 排障 → 工单 → 知识',
        '巡检 → 风险 → 告警 → 排障',
        '质量类 → 告警 → 排障',
        '资源稽核 → 关系修正',
        '变更方案 → 风险评估',
        '割接执行 → 拨测验证'
      ],
      axisLine: { show: false },
      axisTick: { show: false },
      axisLabel: { color: C.ink, fontSize: 12, fontFamily: FONT }
    },
    series: [
      {
        type: 'bar',
        barWidth: 14,
        data: [95, 85, 80, 80, 70, 20],
        itemStyle: {
          borderRadius: 5,
          color: function (params) {
            if (params.dataIndex === 0) return C.accent2;
            if (params.dataIndex === 5) return C.amber;
            return C.accent;
          }
        },
        label: {
          show: true,
          position: 'right',
          color: C.muted,
          fontSize: 11,
          fontFamily: FONT,
          formatter: '{c}'
        }
      }
    ]
  });

  /* 3. 接口编排情况：15 / 71 */
  mount('chart-tools', {
    animationDuration: 700,
    tooltip: {
      trigger: 'item',
      backgroundColor: C.bg2,
      borderColor: C.rule,
      textStyle: { color: C.ink, fontSize: 12, fontFamily: FONT },
      formatter: '{b}：{c} 个（{d}%）'
    },
    legend: {
      bottom: 0,
      left: 'center',
      itemWidth: 10,
      itemHeight: 10,
      textStyle: { color: C.muted, fontSize: 12, fontFamily: FONT }
    },
    series: [
      {
        type: 'pie',
        radius: ['56%', '80%'],
        center: ['50%', '45%'],
        avoidLabelOverlap: true,
        itemStyle: { borderColor: C.bg2, borderWidth: 3 },
        label: { show: false },
        emphasis: { scaleSize: 4 },
        data: [
          { value: 15, name: '已编排', itemStyle: { color: C.accent } },
          { value: 56, name: '能力已具备、暂未编排', itemStyle: { color: '#E3E9F2' } }
        ]
      }
    ],
    graphic: [
      {
        type: 'text',
        left: 'center',
        top: '35%',
        style: {
          text: '15 / 71',
          fill: C.ink,
          fontSize: 28,
          fontWeight: 700,
          fontFamily: FONT,
          textAlign: 'center'
        }
      },
      {
        type: 'text',
        left: 'center',
        top: '46%',
        style: {
          text: '个接口已编排',
          fill: C.muted,
          fontSize: 12,
          fontFamily: FONT,
          textAlign: 'center'
        }
      }
    ]
  });

  /* 响应式：容器尺寸变化时重绘 */
  var resizeTimer = null;
  function resizeAll() {
    if (resizeTimer) clearTimeout(resizeTimer);
    resizeTimer = setTimeout(function () {
      charts.forEach(function (chart) {
        chart.resize();
      });
    }, 120);
  }
  window.addEventListener('resize', resizeAll);
  window.addEventListener('load', resizeAll);

  /* Mermaid 结构图初始化 */
  if (window.mermaid) {
    window.mermaid.initialize({
      startOnLoad: true,
      theme: 'base',
      securityLevel: 'loose',
      flowchart: { useMaxWidth: true, htmlLabels: true, curve: 'linear' },
      themeVariables: {
        background: C.bg2,
        primaryColor: C.accentSoft,
        primaryTextColor: C.ink,
        primaryBorderColor: C.accent,
        lineColor: '#8A9BB4',
        secondaryColor: '#E6F6F3',
        tertiaryColor: C.bg,
        fontFamily: FONT,
        fontSize: '15px'
      }
    });
  }
})();
