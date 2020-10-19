import { module, test } from 'qunit';
import { setupRenderingTest } from 'ember-qunit';
import { render } from '@ember/test-helpers';
import { hbs } from 'ember-cli-htmlbars';
import { componentA11yAudit } from 'nomad-ui/tests/helpers/a11y-audit';

module('Integration | Component | das/recommendation-chart', function(hooks) {
  setupRenderingTest(hooks);

  test('it renders a chart for a recommended CPU increase', async function(assert) {
    this.set('resource', 'CPU');
    this.set('current', 1312);
    this.set('recommended', 1919);
    this.set('stats', {});

    await render(
      hbs`<Das::RecommendationChart
            @resource={{resource}}
            @currentValue={{current}}
            @recommendedValue={{recommended}}
            @stats={{stats}}
          />`
    );

    assert.dom('.recommendation-chart.increase').exists();
    assert.dom('.recommendation-chart .resource').hasText('CPU');
    assert.dom('.recommendation-chart .icon-is-arrow-up').exists();
    assert.dom('text.percent').hasText('+46%');
    await componentA11yAudit(this.element, assert);
  });

  test('it renders a chart for a recommended memory decrease', async function(assert) {
    this.set('resource', 'MemoryMB');
    this.set('current', 1919);
    this.set('recommended', 1312);
    this.set('stats', {});

    await render(
      hbs`<Das::RecommendationChart
            @resource={{resource}}
            @currentValue={{current}}
            @recommendedValue={{recommended}}
            @stats={{stats}}
          />`
    );

    assert.dom('.recommendation-chart.decrease').exists();
    assert.dom('.recommendation-chart .resource').hasText('Mem');
    assert.dom('.recommendation-chart .icon-is-arrow-down').exists();
    assert.dom('text.percent').hasText('-32%');
    await componentA11yAudit(this.element, assert);
  });

  test('it handles the maximum being far beyond the recommended', async function(assert) {
    this.set('resource', 'CPU');
    this.set('current', 1312);
    this.set('recommended', 1919);
    this.set('stats', {
      max: 3000,
    });

    await render(
      hbs`<Das::RecommendationChart
            @resource={{resource}}
            @currentValue={{current}}
            @recommendedValue={{recommended}}
            @stats={{stats}}
          />`
    );

    const chartSvg = this.element.querySelector('.recommendation-chart svg');
    const maxLine = chartSvg.querySelector('line.stat.max');

    assert.ok(maxLine.getAttribute('x1') < chartSvg.clientWidth);
  });

  test('it can be disabled and will show no delta', async function(assert) {
    this.set('resource', 'CPU');
    this.set('current', 1312);
    this.set('recommended', 1919);
    this.set('stats', {});

    await render(
      hbs`<Das::RecommendationChart
            @resource={{resource}}
            @currentValue={{current}}
            @recommendedValue={{recommended}}
            @stats={{stats}}
            @disabled={{true}}
          />`
    );

    assert.dom('.recommendation-chart.disabled');
    assert.dom('.recommendation-chart.increase').doesNotExist();
    assert.dom('.recommendation-chart rect.delta').doesNotExist();
    assert.dom('.recommendation-chart .changes').doesNotExist();
    assert.dom('.recommendation-chart .resource').hasText('CPU');
    assert.dom('.recommendation-chart .icon-is-arrow-up').exists();
    await componentA11yAudit(this.element, assert);
  });

  test('the stats labels shift aligment and disappear to account for space', async function(assert) {
    this.set('resource', 'CPU');
    this.set('current', 50);
    this.set('recommended', 100);

    this.set('stats', {
      mean: 5,
      p99: 99,
      max: 100,
    });

    await render(
      hbs`<Das::RecommendationChart
            @resource={{resource}}
            @currentValue={{current}}
            @recommendedValue={{recommended}}
            @stats={{stats}}
          />`
    );

    assert.dom('[data-test-label=max]').hasClass('right');

    this.set('stats', {
      mean: 5,
      p99: 6,
      max: 100,
    });

    assert.dom('[data-test-label=max]').hasNoClass('right');
    assert.dom('[data-test-label=p99]').hasClass('right');

    this.set('stats', {
      mean: 5,
      p99: 6,
      max: 7,
    });

    assert.dom('[data-test-label=max]').hasClass('right');
    assert.dom('[data-test-label=p99]').hasClass('hidden');
  });
});
