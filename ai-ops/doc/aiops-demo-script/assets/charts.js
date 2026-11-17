(function () {
  if (typeof echarts === 'undefined') return;

  var cs = getComputedStyle(document.documentElement);
  function v(name) { return cs.getPropertyValue(name).trim(); }

  var accent = v('--accent');
  var accent2 = v('--accent2');
  var ink = v('--ink');
  var muted = v('--muted');
  var rule = v('--rule');
  var bg2 = v('--bg2');

  var axisCommon = {
    axisLine: { lineStyle: { color: rule } },
    axisTick: { show: false },
    axisLabel: { color: muted, fontSize: 12 },
    splitLine: { lineStyle: { color: rule, type: 'dashed' } }
  };

  // ---- 图 1：各环节建议用时 ----
  var pacingEl = document.getElementById('chart-pacing');
  if (pacingEl) {
    var pacing = echarts.init(pacingEl, null, { renderer: 'svg' });
    pacing.setOption({
      animation: false,
      grid: { left: 8, right: 64, top: 10, bottom: 4, containLabel: true },
      tooltip: {
        trigger: 'axis',
        appendToBody: true,
        axisPointer: { type: 'shadow' },
        formatter: function (p) { return p[0].name + '：' + p[0].value + ' 分钟'; }
      },
      xAxis: {
        type: 'value',
        max: 6.5,
        splitNumber: 3,
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: axisCommon.axisLabel,
        splitLine: axisCommon.splitLine
      },
      yAxis: {
        type: 'category',
        inverse: true,
        data: ['开场', '第一幕 · 全局认知', '第二幕 · 主动发现', '第三幕 · 定位根因', '第四幕 · 处置闭环', '第五幕 · 度量收尾'],
        axisLine: { show: false },
        axisTick: { show: false },
        axisLabel: { color: ink, fontSize: 13 }
      },
      series: [{
        type: 'bar',
        data: [0.5, 2, 6, 4, 4, 2],
        barWidth: 18,
        itemStyle: { color: accent, borderRadius: [0, 4, 4, 0] },
        label: {
          show: true,
          position: 'right',
          color: ink,
          fontSize: 12,
          formatter: function (p) { return p.value + '′'; }
        }
      }]
    });
    window.addEventListener('resize', function () { pacing.resize(); });
  }

  // ---- 图 3：告警来源构成 ----
  var alertsEl = document.getElementById('chart-alerts');
  if (alertsEl) {
    var alerts = echarts.init(alertsEl, null, { renderer: 'svg' });
    alerts.setOption({
      animation: false,
      color: [accent, accent2, accent + 'CC', accent2 + 'B3', accent + '99'],
      tooltip: {
        trigger: 'item',
        appendToBody: true,
        formatter: function (p) { return p.name + '：' + p.value + ' 条（' + p.percent + '%）'; }
      },
      legend: {
        bottom: 0,
        left: 'center',
        icon: 'circle',
        itemWidth: 8,
        itemHeight: 8,
        textStyle: { color: muted, fontSize: 12 }
      },
      series: [{
        type: 'pie',
        radius: ['46%', '68%'],
        center: ['50%', '44%'],
        avoidLabelOverlap: true,
        itemStyle: { borderColor: bg2, borderWidth: 2 },
        label: {
          color: ink,
          fontSize: 12,
          formatter: '{b}\n{c} 条'
        },
        labelLine: { lineStyle: { color: rule } },
        data: [
          { name: '巡检回流', value: 37 },
          { name: '指标劣化', value: 9 },
          { name: 'Prometheus', value: 4 },
          { name: '业务监控', value: 3 },
          { name: '质差分析', value: 1 }
        ]
      }]
    });
    window.addEventListener('resize', function () { alerts.resize(); });
  }
})();
