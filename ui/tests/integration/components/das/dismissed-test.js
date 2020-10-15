import { module, test } from 'qunit';
import { setupRenderingTest } from 'ember-qunit';
import { click, render } from '@ember/test-helpers';
import { hbs } from 'ember-cli-htmlbars';
import { componentA11yAudit } from 'nomad-ui/tests/helpers/a11y-audit';
import { task } from 'ember-concurrency';
import sinon from 'sinon';
import EmberObject from '@ember/object';

module('Integration | Component | das/dismissed', function(hooks) {
  setupRenderingTest(hooks);

  hooks.beforeEach(function() {
    window.localStorage.clear();
  });

  test('it renders the dismissal interstitial with a button to proceed and an option to never show again and proceeds manually', async function(assert) {
    const proceedSpy = sinon.spy();

    const TaskOwnerClass = EmberObject.extend({
      proceed: task(function*(manual) {
        assert.ok(manual);
        proceedSpy();
        yield manual;
        return true;
      }),
    });

    this.set('proceedTaskOwner', TaskOwnerClass.create());

    await render(
      hbs`{{#unless proceedTaskOwner.proceed.lastSuccessful.value}}<Das::Dismissed @proceed={{proceedTaskOwner.proceed}} />{{/unless}}`
    );

    await componentA11yAudit(this.element, assert);

    await click('input[type=checkbox]');
    await click('[data-test-understood]');

    assert.ok(proceedSpy.called);
    assert.equal(window.localStorage.getItem('nomadRecommendationDismssalUnderstood'), 'true');
  });

  test('it renders the dismissal interstitial with no button when the option to never show again has been chosen and proceeds automatically', async function(assert) {
    window.localStorage.setItem('nomadRecommendationDismssalUnderstood', true);

    const TaskOwnerClass = EmberObject.extend({
      proceed: task(function*(manual) {
        assert.notOk(manual);
        yield true;
      }),
    });

    this.set('proceedTaskOwner', TaskOwnerClass.create());

    await render(hbs`<Das::Dismissed @proceed={{proceedTaskOwner.proceed}} />`);

    assert.dom('[data-test-understood]').doesNotExist();

    await componentA11yAudit(this.element, assert);
  });
});
