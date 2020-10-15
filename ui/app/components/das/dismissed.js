import Component from '@glimmer/component';
import localStorageProperty from 'nomad-ui/utils/properties/local-storage';
import { tracked } from '@glimmer/tracking';
import { action } from '@ember/object';

export default class DasDismissedComponent extends Component {
  @localStorageProperty('nomadRecommendationDismssalUnderstood', false) explanationUnderstood;

  @tracked dismissInTheFuture = false;

  @action
  proceedManually() {
    const manualPromise = new Promise(resolve => {
      this.manualPromiseResolve = resolve;
    });

    this.args.proceed.perform(manualPromise);
  }

  @action
  understoodClicked() {
    this.explanationUnderstood = this.dismissInTheFuture;
    this.manualPromiseResolve(true);
  }
}
